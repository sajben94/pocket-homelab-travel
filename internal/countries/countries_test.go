package countries

import "testing"

func TestData(t *testing.T) {
	if n := len(All()); n < 240 {
		t.Fatalf("only %d countries", n)
	}
	ba := Get("ba")
	if ba == nil || ba.Name != "Bosnia and Herzegovina" || ba.Capital[0] != "Sarajevo" || ba.Currencies[0].Code != "BAM" || ba.Drives != "right" || ba.CallingCode != "+387" {
		t.Fatalf("BA = %+v", ba)
	}
	if gb := Get("GB"); gb == nil || gb.Drives != "left" || gb.Population < 60_000_000 {
		t.Fatalf("GB = %+v", gb)
	}
	if Get("ZZ") != nil {
		t.Fatal("ZZ exists")
	}
}
