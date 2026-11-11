package adapters

import (
	"errors"
	"github.com/np1-control-center/ncc/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAlexaRequestStrictSchemaAndExpectedEnum(t *testing.T) {
	for _, body := range []string{`{}`, `{"expectedValue":null,"enabled":true}`, `{"expectedValue":0,"enabled":null}`, `{"expectedValue":true,"enabled":false}`, `{"expectedValue":1.0,"enabled":true}`, `{"expectedValue":3,"enabled":true}`, `{"expectedValue":0,"enabled":"false"}`, `{"expectedValue":0,"enabled":false,"path":"/tmp/other"}`, `{"expectedValue":0,"enabled":false} {}`} {
		called := false
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/alexa-settings", strings.NewReader(body))
		handleAlexa(w, r, func(*alexaPut) (model.Observation, error) { called = true; return model.Observation{}, nil })
		if w.Code != 400 || called {
			t.Errorf("unsafe Alexa request reached action status=%d", w.Code)
		}
	}
}
func TestAlexaZeroIsValidAndFixedIsReadOnly(t *testing.T) {
	called := false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/alexa-settings", strings.NewReader(`{"expectedValue":0,"enabled":false}`))
	handleAlexa(w, r, func(p *alexaPut) (model.Observation, error) {
		called = p.ExpectedValue == 0 && !p.Enabled
		return alexaObservation(1), nil
	})
	if w.Code != 200 || !called {
		t.Fatal("zero enum not accepted")
	}
	fixed := alexaObservation(2).Data.(AlexaSettingsData)
	if fixed.Writable || !fixed.WakeWord.Fixed || !fixed.WakeWord.Enabled {
		t.Fatal("stock linkage guard bypassed in presentation")
	}
}
func TestAlexaHelperRequiresValueAndCleanupRatherThanDefaultingEmptyToON(t *testing.T) {
	for _, body := range []string{"", "alexa-wire=0\n", "cleanup=matched; own-receiver-removed\n", "alexa-wire=3\ncleanup=matched; own-receiver-removed\n", "alexa-wire=0\nalexa-wire=1\ncleanup=matched; own-receiver-removed\n"} {
		if _, e := parseAlexaHelperOutput([]byte(body)); e == nil {
			t.Fatal("unconfirmed response accepted")
		}
	}
	for _, value := range []string{"0", "1", "2"} {
		got, e := parseAlexaHelperOutput([]byte("alexa-wire=" + value + "\ncleanup=matched; own-receiver-removed\n"))
		if e != nil || got != int(value[0]-'0') {
			t.Fatal("confirmed result rejected")
		}
	}
}
func TestAlexaConflictHasSpecificStatusAndNoFalseSuccess(t *testing.T) {
	for _, failure := range []error{errAlexaConflict, errAlexaFixed, errors.New("worker failed")} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/alexa-settings", nil)
		handleAlexa(w, r, func(*alexaPut) (model.Observation, error) { return model.Observation{}, failure })
		if w.Code == 200 {
			t.Fatal("failed action shown successful")
		}
		if strings.Contains(failure.Error(), "ALEXA_") && w.Code != 409 {
			t.Fatal("guard/conflict needs refresh status")
		}
	}
}
