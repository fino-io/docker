package docker

import "testing"

func Test_ImageNameRegexp(t *testing.T) {
	tests := []struct {
		name      string
		imageName string
		want      bool
	}{
		{
			name:      "1",
			imageName: "demo-linux-amd64-ubuntu-20_04.2vunrmty7ouoyemzpjj77bylyeo",
			want:      true,
		}, {
			name:      "2",
			imageName: "demo-linux-amd64-ubuntu-20_04.2vthdbseqzvwl3r21t2ste1b3gs",
			want:      true,
		}, {
			name:      "3",
			imageName: "demo-linux-amd64-ubuntu-20_04",
			want:      false,
		}, {
			name:      "4",
			imageName: "demo-linux-amd64-ubuntu-20_0",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := demoImageName.MatchString(tt.imageName); got != tt.want {
				t.Errorf("not matched, got %v, want %v", got, tt.want)
			}
		})
	}
}
