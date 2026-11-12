package platespeech

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/plate"
)

// Synthesizer is the API side; Player plays PCM on the device.
type Synthesizer interface {
	Synthesize(ctx context.Context, api API, text string, style int) ([]byte, error)
	Styles(ctx context.Context, api API) ([]Style, error)
	Points(ctx context.Context, api API) (Points, error)
}

type Player interface {
	Available() error
	Play(ctx context.Context, pcm []byte, volume int) error
}

type Notifier interface {
	Start(context.Context, led.Notification, time.Duration) (func() error, error)
}

// Part of an announcement: an uploaded clip or synthesized text.
type Part struct {
	Upload string
	Text   string
}

// Plan composes the announcement for a rule match: the rule's message
// (uploaded clip, or text synthesized together with the fixed phrase), then
// the plate. An exact rule whose reading matches is synthesized as one
// sentence for natural prosody; otherwise the plate is read from vocabulary
// clips.
func Plan(rule plate.Rule, reading plate.Reading, voice plate.Voice) []Part {
	parts := []Part{}
	lead := voice.Phrase
	if rule.MessageAudio != "" {
		parts = append(parts, Part{Upload: rule.MessageAudio})
	} else {
		lead = rule.Message + voice.Phrase
	}
	if rule.Exact() && reading.Region == rule.Region && reading.Class == rule.Class && reading.Kana == rule.Kana && reading.Number == rule.Number {
		return append(parts, Part{Text: lead + Sentence(reading)})
	}
	if lead != "" {
		parts = append(parts, Part{Text: lead})
	}
	for _, t := range Tokens(reading) {
		parts = append(parts, Part{Text: t})
	}
	return parts
}

// ruleExample is the reading used to prepare and preview a rule: its own
// fields, with wildcards shown as 1.
func ruleExample(rule plate.Rule) plate.Reading {
	fill := func(s string) string { return strings.ReplaceAll(s, "*", "1") }
	return plate.Reading{Region: rule.Region, Class: fill(rule.Class), Kana: rule.Kana, Number: strings.TrimLeft(fill(rule.Number), "0")}
}

// leadTexts are the per-rule texts synthesized when settings are saved.
func leadTexts(rule plate.Rule, voice plate.Voice) []string {
	out := []string{}
	for _, p := range Plan(rule, ruleExample(rule), voice) {
		if p.Text != "" {
			out = append(out, p.Text)
			if !rule.Exact() {
				break // only the lead; the plate comes from vocabulary
			}
		}
	}
	return out
}

// Job reports clip generation.
type Job struct {
	State     string    `json:"state"` // idle, generating, ready, no-key, error
	Total     int       `json:"total"`
	Ready     int       `json:"ready"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RuleClip is the preparation state of one speaking rule.
type RuleClip struct {
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

type Status struct {
	KeyConfigured bool                `json:"keyConfigured"`
	Player        string              `json:"player,omitempty"` // non-empty when the device cannot play
	Job           Job                 `json:"job"`
	Rules         map[string]RuleClip `json:"rules"`
	Style         *Style              `json:"style,omitempty"`
	Credit        string              `json:"credit"`
}

// Manager owns the clip store under dir:
//
//	api.json     endpoint and key (0600)
//	clips/<style>/<hash>.pcm   synthesized text
//	uploads/<id>.pcm           owner uploads
//	styles.json  cached style list
type Manager struct {
	Notifier Notifier
	dir      string
	synth    Synthesizer
	player   Player
	now      func() time.Time
	pace     time.Duration

	mu       sync.Mutex
	api      API
	voice    plate.Voice
	rules    []plate.Rule
	priority []string // rule texts, generated first
	vocab    []string
	job      Job
	failures map[string]string // text -> last error
	gen      uint64            // bumped when what to generate changes
	styles   []Style
	wake     chan struct{}
	queue    chan request
}

type request struct {
	ctx   context.Context
	plan  []Part
	vol   int
	light led.Notification
	done  func(error)
}

func NewManager(dir string, synth Synthesizer, player Player) *Manager {
	m := &Manager{dir: dir, synth: synth, player: player, now: time.Now, pace: 1500 * time.Millisecond,
		voice: plate.DefaultVoice(), failures: map[string]string{}, wake: make(chan struct{}, 1), queue: make(chan request, 4)}
	m.api = API{Endpoint: DefaultAPIBase}
	if b, e := os.ReadFile(filepath.Join(dir, "api.json")); e == nil {
		var a API
		if json.Unmarshal(b, &a) == nil {
			if ep, e := ValidateEndpoint(a.Endpoint); e == nil {
				m.api = API{Endpoint: ep, Key: a.Key}
			}
		}
	}
	if b, e := os.ReadFile(filepath.Join(dir, "styles.json")); e == nil {
		_ = json.Unmarshal(b, &m.styles)
	}
	m.job = Job{State: "idle", UpdatedAt: m.now().UTC()}
	return m
}

func (m *Manager) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func hashName(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:10]) + ".pcm"
}

func (m *Manager) clipPath(style int, text string) string {
	return filepath.Join(m.clipRoot(), strconv.Itoa(style), hashName(text))
}

// Older clips used a POST-only speaker parameter, which the provider ignored.
// Keep them for rollback, but never reuse them as the selected voice.
func (m *Manager) clipRoot() string { return filepath.Join(m.dir, "clips-speaker-query-v2") }

func (m *Manager) uploadPath(id string) string { return filepath.Join(m.dir, "uploads", id+".pcm") }

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if e != nil {
		return e
	}
	_, e = tmp.Write(data)
	if e == nil {
		e = tmp.Chmod(mode)
	}
	if e == nil {
		e = tmp.Sync()
	}
	if c := tmp.Close(); e == nil {
		e = c
	}
	if e == nil {
		e = os.Rename(tmp.Name(), path)
	}
	if e != nil {
		_ = os.Remove(tmp.Name())
	}
	return e
}

// APIConfig returns the endpoint and whether a key is stored. The key
// itself is never returned to clients.
func (m *Manager) APIConfig() (endpoint string, keyConfigured bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.api.Endpoint, m.api.Key != ""
}

// SetAPI stores the endpoint and, when key is non-nil, the key ("" clears).
func (m *Manager) SetAPI(endpoint string, key *string) error {
	ep, e := ValidateEndpoint(endpoint)
	if e != nil {
		return e
	}
	m.mu.Lock()
	next := API{Endpoint: ep, Key: m.api.Key}
	if key != nil {
		next.Key = strings.TrimSpace(*key)
	}
	changed := next.Endpoint != m.api.Endpoint
	m.mu.Unlock()
	if strings.ContainsAny(next.Key, " \t\r\n") || len(next.Key) > 200 {
		return errors.New("APIキーの形式が正しくありません")
	}
	b, _ := json.Marshal(next)
	if e = writeAtomic(filepath.Join(m.dir, "api.json"), b, 0o600); e != nil {
		return e
	}
	m.mu.Lock()
	m.api = next
	m.failures = map[string]string{}
	m.gen++
	if changed {
		m.styles = nil
		_ = os.Remove(filepath.Join(m.dir, "styles.json"))
	}
	m.mu.Unlock()
	m.kick()
	return nil
}

// CheckAPI asks the endpoint for the remaining points with the stored key.
func (m *Manager) CheckAPI(ctx context.Context) (Points, error) {
	m.mu.Lock()
	api := m.api
	m.mu.Unlock()
	if api.Key == "" {
		return Points{}, errors.New("VOICEVOX APIキーが未設定です")
	}
	p, e := m.synth.Points(ctx, api)
	if e != nil {
		return p, errors.New(describe(e))
	}
	return p, nil
}

// SaveUpload stores an owner clip and returns its ID.
func (m *Manager) SaveUpload(pcm []byte) (string, error) {
	h := sha256.Sum256(pcm)
	id := hex.EncodeToString(h[:10])
	if e := writeAtomic(m.uploadPath(id), pcm, 0o600); e != nil {
		return "", e
	}
	return id, nil
}

func (m *Manager) HasUpload(id string) bool {
	if !plate.AudioIDPattern.MatchString(id) {
		return false
	}
	_, e := os.Stat(m.uploadPath(id))
	return e == nil
}

// Configure takes the saved settings: it schedules synthesis of every
// speaking rule's lead and the vocabulary for the chosen style, and removes
// clips nothing uses any more.
func (m *Manager) Configure(settings plate.Settings) {
	priority := []string{}
	referenced := map[string]bool{}
	rules := []plate.Rule{}
	for _, r := range settings.Rules {
		if !r.Speak {
			continue
		}
		rules = append(rules, r)
		priority = append(priority, leadTexts(r, settings.Voice)...)
		if r.MessageAudio != "" {
			referenced[r.MessageAudio] = true
		}
	}
	m.mu.Lock()
	m.voice, m.rules, m.priority, m.vocab = settings.Voice, rules, priority, Vocabulary()
	m.failures = map[string]string{}
	m.gen++
	keep := map[string]bool{}
	for _, t := range append(append([]string{}, priority...), m.vocab...) {
		keep[hashName(t)] = true
	}
	style := settings.Voice.Style
	m.mu.Unlock()
	// Clips of other styles and of edited messages are not reused.
	if entries, e := os.ReadDir(m.clipRoot()); e == nil {
		for _, d := range entries {
			if d.Name() != strconv.Itoa(style) {
				_ = os.RemoveAll(filepath.Join(m.clipRoot(), d.Name()))
			}
		}
	}
	if files, e := os.ReadDir(filepath.Join(m.clipRoot(), strconv.Itoa(style))); e == nil {
		for _, f := range files {
			if !keep[f.Name()] {
				_ = os.Remove(filepath.Join(m.clipRoot(), strconv.Itoa(style), f.Name()))
			}
		}
	}
	// Unreferenced uploads are kept for a day so a rule being edited can
	// still be saved with a clip uploaded before the save.
	if files, e := os.ReadDir(filepath.Join(m.dir, "uploads")); e == nil {
		for _, f := range files {
			id := strings.TrimSuffix(f.Name(), ".pcm")
			if info, e := f.Info(); e == nil && !referenced[id] && m.now().Sub(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(m.dir, "uploads", f.Name()))
			}
		}
	}
	m.kick()
}

func (m *Manager) missing() (api API, style int, todo []string, total int) {
	api, style, todo, total, _ = m.snapshot()
	return
}

func (m *Manager) snapshot() (api API, style int, todo []string, total int, gen uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	gen = m.gen
	all := append(append([]string{}, m.priority...), m.vocab...)
	seen := map[string]bool{}
	for _, t := range all {
		if seen[t] {
			continue
		}
		seen[t] = true
		total++
		if _, e := os.Stat(m.clipPath(m.voice.Style, t)); e != nil && m.failures[t] == "" {
			todo = append(todo, t)
		}
	}
	return m.api, m.voice.Style, todo, total, gen
}

func (m *Manager) current(gen uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen == gen
}

func (m *Manager) setJob(state string, total, ready int, err string) {
	m.mu.Lock()
	m.job = Job{State: state, Total: total, Ready: ready, Error: err, UpdatedAt: m.now().UTC()}
	m.mu.Unlock()
}

func describe(e error) string {
	var ae *APIError
	if errors.As(e, &ae) {
		switch ae.Code {
		case "invalidApiKey":
			return "APIキーが無効です"
		case "notEnoughPoints":
			return "APIのポイントが不足しています（翌日に回復します）"
		case "failed":
			return "APIが音声合成に失敗しました"
		}
		if ae.Status == 429 {
			return "APIの利用制限中です（しばらくして再試行します）"
		}
		return "APIエラー（" + ae.Code + "）"
	}
	return "APIに接続できません"
}

// generate synthesizes missing clips one at a time, paced so the shared
// API is not flooded. It returns how long to wait before the next attempt.
func (m *Manager) generate(ctx context.Context) time.Duration {
	api, style, todo, total, gen := m.snapshot()
	if total == 0 {
		m.setJob("idle", 0, 0, "")
		return time.Hour
	}
	if len(todo) == 0 {
		m.mu.Lock()
		failed := len(m.failures)
		// API failures are often transient (serverError); forget them so
		// the next round retries.
		m.failures = map[string]string{}
		m.mu.Unlock()
		if failed > 0 {
			m.setJob("error", total, total-failed, fmt.Sprintf("%d件の音声を生成できませんでした（30分後に再試行します）", failed))
			return 30 * time.Minute
		}
		m.setJob("ready", total, total, "")
		m.refreshStyles(ctx, api)
		return time.Hour
	}
	if api.Key == "" {
		m.setJob("no-key", total, total-len(todo), "VOICEVOX APIキーが未設定です")
		return time.Hour
	}
	ready := total - len(todo)
	for _, text := range todo {
		// Saved settings or a new key re-prioritize: rule messages first.
		if !m.current(gen) {
			return 0
		}
		m.setJob("generating", total, ready, "")
		pcm, e := m.synth.Synthesize(ctx, api, text, style)
		if ctx.Err() != nil {
			return 0
		}
		if e != nil {
			var ae *APIError
			if errors.As(e, &ae) && ae.Status == 429 {
				// The shared API rate-limits bursts; wait and continue.
				m.setJob("waiting", total, ready, "APIの利用制限のため一時停止中（自動で再開します）")
				return 2 * time.Minute
			}
			if errors.As(e, &ae) && (ae.Permanent() || ae.Code == "notEnoughPoints") {
				m.setJob("error", total, ready, describe(e))
				if ae.Permanent() {
					return 24 * time.Hour // until the key changes (SetKey wakes us)
				}
				return time.Hour
			}
			if errors.As(e, &ae) {
				m.mu.Lock()
				m.failures[text] = describe(e)
				m.mu.Unlock()
				continue
			}
			m.setJob("error", total, ready, describe(e))
			return time.Minute
		}
		// Settings may have changed while the request was running.
		if !m.current(gen) {
			if curAPI, curStyle, _, _ := m.missing(); curStyle != style || curAPI != api {
				return 0
			}
		}
		if e = writeAtomic(m.clipPath(style, text), pcm, 0o600); e != nil {
			m.setJob("error", total, ready, "音声を保存できません")
			return time.Minute
		}
		ready++
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(m.pace):
		}
	}
	return 0
}

func (m *Manager) refreshStyles(ctx context.Context, api API) {
	info, e := os.Stat(filepath.Join(m.dir, "styles.json"))
	if api.Key == "" || (e == nil && m.now().Sub(info.ModTime()) < 24*time.Hour) {
		return
	}
	styles, e := m.synth.Styles(ctx, api)
	if e != nil || len(styles) == 0 {
		return
	}
	if b, e := json.Marshal(styles); e == nil {
		_ = writeAtomic(filepath.Join(m.dir, "styles.json"), b, 0o600)
	}
	m.mu.Lock()
	m.styles = styles
	m.mu.Unlock()
}

// Styles returns the cached list (fetching it once when empty).
func (m *Manager) Styles(ctx context.Context) []Style {
	m.mu.Lock()
	styles, api := m.styles, m.api
	m.mu.Unlock()
	if len(styles) == 0 && api.Key != "" {
		_ = os.Remove(filepath.Join(m.dir, "styles.json"))
		m.refreshStyles(ctx, api)
		m.mu.Lock()
		styles = m.styles
		m.mu.Unlock()
	}
	return append([]Style(nil), styles...)
}

// Run generates clips and plays queued announcements until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-m.queue:
				playCtx, cancel := context.WithCancel(r.ctx)
				stop := context.AfterFunc(ctx, cancel)
				r.done(m.playNotification(playCtx, r.plan, r.vol, r.light))
				stop()
				cancel()
			}
		}
	}()
	for {
		wait := m.generate(ctx)
		if wait == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-time.After(wait):
		}
	}
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{KeyConfigured: m.api.Key != "", Job: m.job, Rules: map[string]RuleClip{}, Credit: "VOICEVOX"}
	if e := m.player.Available(); e != nil {
		s.Player = e.Error()
	}
	for _, r := range m.rules {
		c := RuleClip{Ready: true}
		if r.MessageAudio != "" {
			if _, e := os.Stat(m.uploadPath(r.MessageAudio)); e != nil {
				c = RuleClip{Error: "アップロードした音声がありません"}
			}
		}
		for _, t := range leadTexts(r, m.voice) {
			if _, e := os.Stat(m.clipPath(m.voice.Style, t)); e != nil {
				c.Ready = false
				if f := m.failures[t]; f != "" {
					c.Error = f
				}
			}
		}
		s.Rules[r.ID] = c
	}
	for _, st := range m.styles {
		if st.ID == m.voice.Style {
			st := st
			s.Style = &st
			s.Credit = "VOICEVOX:" + st.Speaker
		}
	}
	if s.Style == nil && m.voice.Style == 1 {
		s.Credit = "VOICEVOX:ずんだもん"
	}
	return s
}

// Available reports whether announcements can be played at all.
func (m *Manager) Available() (bool, string) {
	if e := m.player.Available(); e != nil {
		return false, e.Error()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.api.Key == "" {
		return false, "VOICEVOX APIキーが未設定です"
	}
	return true, ""
}

func (m *Manager) load(ctx context.Context, p Part, style int) ([]byte, error) {
	if p.Upload != "" {
		b, e := os.ReadFile(m.uploadPath(p.Upload))
		if e != nil {
			return nil, errors.New("アップロードした音声がありません")
		}
		return b, nil
	}
	if b, e := os.ReadFile(m.clipPath(style, p.Text)); e == nil {
		return b, nil
	}
	// A missing piece (e.g. vocabulary not generated yet) is synthesized on
	// demand. Pieces are a message, the phrase or one plate token, never a
	// whole plate of a detected vehicle.
	m.mu.Lock()
	api := m.api
	m.mu.Unlock()
	if api.Key == "" {
		return nil, errors.New("VOICEVOX APIキーが未設定です")
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pcm, e := m.synth.Synthesize(cctx, api, p.Text, style)
	if e != nil {
		return nil, errors.New(describe(e))
	}
	_ = writeAtomic(m.clipPath(style, p.Text), pcm, 0o600)
	return pcm, nil
}

// render concatenates the plan's clips with short pauses.
func (m *Manager) render(ctx context.Context, plan []Part) ([]byte, error) {
	m.mu.Lock()
	style := m.voice.Style
	m.mu.Unlock()
	var pcm []byte
	for i, p := range plan {
		b, e := m.load(ctx, p, style)
		if e != nil {
			return nil, e
		}
		if i > 0 {
			gap := 90 // between plate tokens
			if i == 1 {
				gap = 250 // after the message
			}
			pcm = append(pcm, silence(gap)...)
		}
		pcm = append(pcm, b...)
	}
	if len(pcm) == 0 {
		return nil, errors.New("発話する内容がありません")
	}
	return append(pcm, silence(200)...), nil
}

func (m *Manager) play(ctx context.Context, plan []Part, volume int) error {
	return m.playNotification(ctx, plan, volume, led.Notification{})
}

func (m *Manager) playNotification(ctx context.Context, plan []Part, volume int, light led.Notification) error {
	var pcm []byte
	var e error
	if len(plan) > 0 {
		pcm, e = m.render(ctx, plan)
	}
	var finish func() error
	if light.Enabled() {
		if m.Notifier == nil {
			e = errors.Join(e, errors.New("LED通知を利用できません"))
		} else {
			// Safety lease also covers a stalled audio process; finish starts the
			// two-second tail after the actual PulseAudio drain completes.
			var lightError error
			finish, lightError = m.Notifier.Start(ctx, light, time.Duration(len(pcm))*time.Second/bytesPerSec+12*time.Second)
			e = errors.Join(e, lightError)
		}
	}
	// A failed light must not suppress the requested announcement.
	if len(pcm) > 0 {
		e = errors.Join(e, m.player.Play(ctx, pcm, volume))
	}
	if finish != nil {
		e = errors.Join(e, finish())
	}
	return e
}

// PreviewAudio returns a rule's announcement (its own fields as the plate)
// as PCM, for listening in the browser.
func (m *Manager) PreviewAudio(ctx context.Context, rule plate.Rule) ([]byte, error) {
	m.mu.Lock()
	voice := m.voice
	m.mu.Unlock()
	return m.render(ctx, Plan(rule, ruleExample(rule), voice))
}

// Announce queues a match announcement; done receives the outcome. It
// returns false when the queue is full (the announcement is dropped).
func (m *Manager) Announce(ctx context.Context, rule plate.Rule, reading plate.Reading, done func(error)) bool {
	m.mu.Lock()
	voice := m.voice
	m.mu.Unlock()
	var plan []Part
	if rule.Speak {
		plan = Plan(rule, reading, voice)
	}
	select {
	case m.queue <- request{ctx: ctx, plan: plan, vol: voice.Volume, light: rule.LED, done: done}:
		return true
	default:
		return false
	}
}

// Preview plays a rule with its own fields as the plate and waits.
func (m *Manager) Preview(ctx context.Context, rule plate.Rule) error {
	result := make(chan error, 1)
	if !m.Announce(ctx, rule, ruleExample(rule), func(e error) { result <- e }) {
		return errors.New("発話が混み合っています")
	}
	select {
	case e := <-result:
		return e
	case <-ctx.Done():
		return ctx.Err()
	}
}
