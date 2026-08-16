package domain

import "testing"

func TestValidateFixedPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []Port
	}{
		{name: "unsafe name", ports: []Port{{Name: "../ssh", Port: 2222, Protocol: "tcp"}}},
		{name: "dynamic", ports: []Port{{Name: "ssh", Protocol: "tcp"}}},
		{name: "protocol", ports: []Port{{Name: "ssh", Port: 2222, Protocol: "smtp"}}},
		{name: "duplicate name", ports: []Port{{Name: "ssh", Port: 2222, Protocol: "tcp"}, {Name: "ssh", Port: 2223, Protocol: "tcp"}}},
		{name: "duplicate binding", ports: []Port{{Name: "ssh", Port: 2222, Protocol: "tcp"}, {Name: "shell", Port: 2222, Protocol: "tcp"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateFixedPorts(test.ports); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := ValidateFixedPorts([]Port{{Name: "ssh", Port: 2222, Protocol: "tcp"}}); err != nil {
		t.Fatal(err)
	}
}
