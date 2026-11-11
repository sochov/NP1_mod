package adapters

// NativeValueOption describes a proven storage value, never a protobuf/cloud value.
// These labels are valid only after the caller's firmware/layout checks succeed.
// Options do not grant write permission; apply/read-back verification is separate.
type NativeValueOption struct {
	Value int64  `json:"value"`
	Label string `json:"label"`
}

// NativeStorageOptions returns only choices with both ingestion and consumer evidence.
// Unknown enum domains deliberately return nil. Known voice choices name the
// selector, not cached audio availability; writes require the pinned native worker.
func NativeStorageOptions(index int) []NativeValueOption {
	switch index {
	case 8, 50:
		return []NativeValueOption{{0, "ON"}, {1, "OFF"}}
	case 39:
		return []NativeValueOption{{0, "有効"}, {1, "無効"}}
	case 49:
		return []NativeValueOption{{0, "許可"}, {1, "許可しない"}}
	case 51:
		return []NativeValueOption{{0, "低"}, {1, "標準"}, {2, "高"}}
	case 61:
		return []NativeValueOption{{0, "自動で確認する"}, {1, "自動で確認しない"}}
	case 63, 64:
		return []NativeValueOption{{0, "考慮する"}, {1, "考慮しない"}}
	case 65, 66, 67:
		return []NativeValueOption{{0, "利用する"}, {1, "利用しない"}}
	case 68:
		// Storage order differs from the SDK priority enum and the app's display order.
		return []NativeValueOption{{5, "あなたに合わせる"}, {0, "おすすめ"}, {1, "距離が短い"}, {2, "道が広い"}, {3, "到着が早い"}, {4, "料金が安い"}}
	case 77, 78:
		return []NativeValueOption{{0, "Mizuki"}, {1, "Takumi"}, {2, "Tsutomu"}, {3, "Sakura"}, {4, "YukiAoi01"}, {5, "YukiAoi02"}}
	case 79, 80:
		return []NativeValueOption{{0, "通知しない"}, {1, "効果音と音声"}, {2, "効果音のみ"}}
	default:
		return nil
	}
}

// NativeStorageLabel must not be replaced by a generic zero/nonzero boolean conversion.
func NativeStorageLabel(index int, value int64) (string, bool) {
	for _, option := range NativeStorageOptions(index) {
		if option.Value == value {
			return option.Label, true
		}
	}
	return "", false
}
