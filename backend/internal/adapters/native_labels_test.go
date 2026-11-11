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
	} {
		if label, ok := NativeStorageLabel(test.index, test.value); !ok || label != test.label {
			t.Fatalf("storage ID%d value%d: %q known=%v", test.index, test.value, label, ok)
		}
	}
}
func TestNativeStorageLabelsDoNotInventUnknownEnums(t *testing.T) {
	for _, index := range []int{44, 62, 68, 77, 78, 79, 80, 999} {
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
