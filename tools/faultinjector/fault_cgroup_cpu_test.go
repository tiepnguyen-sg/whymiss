package main

import "testing"

func TestQuotaAndPeriod(t *testing.T) {
	for _, tc := range []struct {
		name       string
		percent    float64
		wantQuota  int64
		wantPeriod int64
	}{
		{"7 percent, default period", 7, 7000, 100000},
		{"1 percent, at the floor exactly", 1, 1000, 100000},
		{"0.1 percent, widens the period", 0.1, 1000, 1000000},
		{"0.01 percent, widens the period further", 0.01, 1000, 10000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota, period := quotaAndPeriod(tc.percent)
			if quota != tc.wantQuota || period != tc.wantPeriod {
				t.Fatalf("quotaAndPeriod(%v) = (%d, %d), want (%d, %d)", tc.percent, quota, period, tc.wantQuota, tc.wantPeriod)
			}
			if quota < cgroupCPUMinQuotaUS {
				t.Fatalf("quotaAndPeriod(%v) returned quota %d below the kernel's observed floor %d", tc.percent, quota, cgroupCPUMinQuotaUS)
			}
		})
	}
}
