package main

import "testing"

func TestParsePSISomeAvg10(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{
			name: "typical two-line PSI file",
			input: "some avg10=1.23 avg60=0.45 avg300=0.01 total=123456\n" +
				"full avg10=0.98 avg60=0.30 avg300=0.00 total=98765\n",
			want: 1.23,
		},
		{
			name:  "zero pressure",
			input: "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
			want:  0,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no some line",
			input:   "full avg10=1.00 avg60=1.00 avg300=1.00 total=1\n",
			wantErr: true,
		},
		{
			name:    "some line missing avg10 field",
			input:   "some avg60=0.45 avg300=0.01 total=123456\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parsePSISomeAvg10(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parsePSISomeAvg10() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("parsePSISomeAvg10() = %v, want %v", got, tc.want)
			}
		})
	}
}
