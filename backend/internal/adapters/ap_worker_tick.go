package adapters

import "context"

// Keep policy reload and the next published verification in the same active
// iteration. The AP can remain running for weeks without re-entering startup.
type apSteadyTickIO struct {
	reload  func(*apRuntime, *apConfiguration)
	channel func(context.Context) (int, error)
	verify  func(context.Context) error
	stop    func() error
}

func (r *apRuntime) steadyTick(ctx context.Context, c *apConfiguration, channel int) (bool, error) {
	return apSteadyTick(ctx, r, c, channel, apSteadyTickIO{
		reload: apReloadPreference,
		channel: func(ctx context.Context) (int, error) {
			return apChannel(ctx, r.helper, false)
		},
		verify: r.verify,
		stop:   r.stop,
	})
}

// Returns true only when a genuine radio channel change stopped the AP.
// Preference-only updates never stop hostapd or DHCP.
func apSteadyTick(ctx context.Context, r *apRuntime, c *apConfiguration, channel int, io apSteadyTickIO) (bool, error) {
	io.reload(r, c)
	associated, observationError := io.channel(ctx)
	if observationError != nil {
		associated = 0
	}
	decision, err := apChooseChannel(channel, associated, 0, c.StandaloneFallback)
	if err != nil {
		return false, err
	}
	r.state.ChannelSource = decision.Source
	if decision.Channel != channel {
		return true, io.stop()
	}
	return false, io.verify(ctx)
}
