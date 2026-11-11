package adapters

import (
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/otapolicy"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOTAPolicyRejectsMissingUnknownOrWrongTypesBeforeApply(t *testing.T) {
	for _, body := range []string{`{}`, `{"expectedToken":null,"disabled":true}`, `{"expectedToken":"short","disabled":true}`, `{"expectedToken":"` + strings.Repeat("a", 64) + `","disabled":"false"}`, `{"expectedToken":"` + strings.Repeat("a", 64) + `","disabled":false,"path":"/tmp/other"}`, `{"expectedToken":"` + strings.Repeat("a", 64) + `","disabled":false} {}`} {
		called := false
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/device/ota-policy", strings.NewReader(body))
		handleOTAPolicy(w, r, func() model.Observation { return model.Observation{} }, func(string, bool) (otapolicy.Status, error) { called = true; return otapolicy.Status{}, nil })
		if w.Code != 400 || called {
			t.Errorf("unsafe request accepted status=%d", w.Code)
		}
	}
}
func TestOTAPolicyExactBooleanAndTokenReachApply(t *testing.T) {
	called := false
	token := strings.Repeat("b", 64)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/device/ota-policy", strings.NewReader(`{"expectedToken":"`+token+`","disabled":false}`))
	handleOTAPolicy(w, r, func() model.Observation { return model.Observation{} }, func(got string, disabled bool) (otapolicy.Status, error) {
		called = got == token && !disabled
		return otapolicy.Status{Disabled: false, Managed: true}, nil
	})
	if w.Code != 200 || !called {
		t.Fatalf("status=%d called=%t", w.Code, called)
	}
}
