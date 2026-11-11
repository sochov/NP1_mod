//go:build !linux
package modem
import("context";"errors")
func Probe(ctx context.Context)(map[string]string,error){return nil,errors.New("AT observation requires Linux")}
