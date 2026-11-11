package adapters

import "testing"

func TestNativeStorageLabelsPreserveInvertedValues(t *testing.T) {
	for _, test := range []struct {
		index int
		value int64
		label string
	}{
		{8, 0, "ON"}, {8, 1, "OFF"}, {39, 0, "有効"}, {39, 1, "無効"}, {49, 0, "許可"}, {49, 1, "許可しない"},
		{50, 0, "ON"}, {51, 2, "高"}, {61, 0, "自動で確認する"}, {63, 0, "考慮する"}, {64, 1, "考慮しない"},
		{65, 0, "利用する"}, {66, 1, "利用しない"}, {67, 0, "利用する"},
		{68, 0, "おすすめ"}, {68, 1, "距離が短い"}, {68, 2, "道が広い"}, {68, 3, "到着が早い"}, {68, 4, "料金が安い"}, {68, 5, "あなたに合わせる"},
		{79, 0, "通知しない"}, {79, 1, "効果音と音声"}, {79, 2, "効果音のみ"}, {80, 0, "通知しない"}, {80, 1, "効果音と音声"}, {80, 2, "効果音のみ"},
	} {
		if label, ok := NativeStorageLabel(test.index, test.value); !ok || label != test.label {
			t.Fatalf("storage ID%d value%d: %q known=%v", test.index, test.value, label, ok)
		}
	}
}
func TestNativeStorageLabelsDoNotInventUnknownEnums(t *testing.T) {
	for _, index := range []int{44, 62, 77, 78, 999} {
		if len(NativeStorageOptions(index)) != 0 {
			t.Fatalf("ID%d has unproven choices", index)
		}
		for _, value := range []int64{0, 1, 2, 3, 5, 12, 2147483647} {
			if label, ok := NativeStorageLabel(index, value); ok || label != "" {
				t.Fatalf("unknown ID%d value%d labeled %q", index, value, label)
			}
		}
	}
	for _, index := range []int{8, 39, 49, 50, 61, 63, 64, 65, 66, 67} {
		for _, value := range []int64{-1, 2, 32767} {
			if _, ok := NativeStorageLabel(index, value); ok {
				t.Fatalf("out-of-domain ID%d labeled", index)
			}
		}
	}
	for _, index := range []int{79, 80} {
		for _, value := range []int64{-1, 3, 2147483647} {
			if _, ok := NativeStorageLabel(index, value); ok {
				t.Fatalf("out-of-domain ID%d labeled", index)
			}
		}
	}
	for _, value := range []int64{-1, 6, 2147483647} {
		if _, ok := NativeStorageLabel(68, value); ok {
			t.Fatalf("out-of-domain route priority %d labeled", value)
		}
	}
}
func TestNativeStorageOptionsAreNotMutableGlobalState(t *testing.T) {
	first := NativeStorageOptions(8)
	first[0].Label = "corrupted"
	first[0].Value = 99
	label, ok := NativeStorageLabel(8, 0)
	if !ok || label != "ON" {
		t.Fatal("caller modified shared option state")
	}
}
