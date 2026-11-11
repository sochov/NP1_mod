package nasbackup

import "context"

type ProbeResult struct {
	Status            string `json:"status"`
	Protocol          string `json:"protocol"`
	DirectoryReadable bool   `json:"directoryReadable"`
	Writing           bool   `json:"writing"`
}

func Probe(ctx context.Context, c Config) (ProbeResult, error) {
	result := ProbeResult{Status: "unavailable", Protocol: c.ProtocolLabel()}
	remote, err := Connect(ctx, c)
	if err != nil {
		return result, err
	}
	defer remote.Close()
	result.Status, result.DirectoryReadable = "available", true
	return result, nil
}
func (c Config) ProtocolLabel() string {
	switch c.protocol {
	case "sftp":
		return "SFTP"
	case "webdav":
		return "WebDAV"
	default:
		return "SMB2/3"
	}
}
