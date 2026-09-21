package iosaccess_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/abedegno/muesli/internal/iosaccess"
)

type fixtureInterface struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}

type fixtureExpected struct {
	InterfaceName string `json:"interfaceName"`
	Address       string `json:"address"`
	Family        string `json:"family"`
}

type fixtureCase struct {
	Name       string             `json:"name"`
	Interfaces []fixtureInterface `json:"interfaces"`
	Expected   []fixtureExpected  `json:"expected"`
}

type fixtureFile struct {
	Cases []fixtureCase `json:"cases"`
}

func loadFixture(t *testing.T) fixtureFile {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/ios_access_network_candidates.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixtureFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return f
}

// TestEligiblePairsSharedVectors runs the Go/TypeScript shared vectors
// (issue #767 Task 4): both languages must derive identical eligibility with
// no cross-language calls.
func TestEligiblePairsSharedVectors(t *testing.T) {
	f := loadFixture(t)
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var interfaces []iosaccess.RawInterface
			for _, iface := range c.Interfaces {
				interfaces = append(interfaces, iosaccess.RawInterface{Name: iface.Name, Addresses: iface.Addresses})
			}
			got := iosaccess.EligiblePairs(interfaces)
			if len(got) != len(c.Expected) {
				t.Fatalf("got %d pairs, want %d\ngot=%+v\nwant=%+v", len(got), len(c.Expected), got, c.Expected)
			}
			for i, g := range got {
				w := c.Expected[i]
				if g.InterfaceName != w.InterfaceName || g.Address != w.Address || string(g.Family) != w.Family {
					t.Fatalf("pair %d: got %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestEvaluateSelectionOutcome(t *testing.T) {
	if out := iosaccess.Evaluate(nil); out.AutoSelected != nil || len(out.Candidates) != 0 {
		t.Fatalf("zero candidates: %+v", out)
	}
	one := []iosaccess.InterfaceAddressPair{{InterfaceName: "en0", Address: "192.168.1.20", Family: iosaccess.IPv4}}
	if out := iosaccess.Evaluate(one); out.AutoSelected == nil || !out.AutoSelected.Equal(one[0]) {
		t.Fatalf("one candidate should auto-select: %+v", out)
	}
	many := []iosaccess.InterfaceAddressPair{
		{InterfaceName: "en0", Address: "192.168.1.20", Family: iosaccess.IPv4},
		{InterfaceName: "en1", Address: "192.168.86.5", Family: iosaccess.IPv4},
	}
	if out := iosaccess.Evaluate(many); out.AutoSelected != nil {
		t.Fatalf("many candidates must require explicit selection: %+v", out)
	}
}

func TestInterfaceAddressPairEquality(t *testing.T) {
	a := iosaccess.InterfaceAddressPair{InterfaceName: "en0", Address: "192.168.1.20", Family: iosaccess.IPv4}
	same := a
	if !a.Equal(same) {
		t.Fatalf("expected equal")
	}
	movedInterface := iosaccess.InterfaceAddressPair{InterfaceName: "utun0", Address: "192.168.1.20", Family: iosaccess.IPv4}
	if a.Equal(movedInterface) {
		t.Fatalf("same address on a different interface must not be equal")
	}
	differentAddress := iosaccess.InterfaceAddressPair{InterfaceName: "en0", Address: "192.168.1.21", Family: iosaccess.IPv4}
	if a.Equal(differentAddress) {
		t.Fatalf("different address on the same interface must not be equal")
	}
}
