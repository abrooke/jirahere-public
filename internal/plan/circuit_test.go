package plan

import (
	"reflect"
	"strings"
	"testing"
)

func its(pairs ...string) []Item {
	var out []Item
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Item{Key: pairs[i], Summary: pairs[i+1]})
	}
	return out
}

func zoneNames(o Order) []string {
	var n []string
	for _, z := range o.Zones {
		n = append(n, z.Name)
	}
	return n
}

func TestCircuitClosedLoop(t *testing.T) {
	o, err := Circuit(its(
		"P-3", "10 min. Tidy: Garage :: Laundry Q4'26",
		"P-1", "10 min. Tidy: Laundry :: Kitchen Q4'26",
		"P-2", "10 min. Tidy: Kitchen :: Garage Q4'26",
	))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Closed {
		t.Error("want closed loop")
	}
	if got, want := zoneNames(o), []string{"Laundry", "Kitchen", "Garage"}; !reflect.DeepEqual(got, want) {
		t.Errorf("zones = %v, want %v", got, want)
	}
	want := []Link{
		{"Laundry", "Kitchen", "P-1", "P-2"},
		{"Kitchen", "Garage", "P-2", "P-3"},
		{"Garage", "Laundry", "P-3", "P-1"},
	}
	if !reflect.DeepEqual(o.Links, want) {
		t.Errorf("links = %v, want %v", o.Links, want)
	}
}

func TestCircuitOpenChain(t *testing.T) {
	o, err := Circuit(its(
		"P-2", "B :: C Q1'27",
		"P-1", "A :: B",
		"P-3", "C",
		"P-9", "Unrelated",
	))
	if err != nil {
		t.Fatal(err)
	}
	if o.Closed {
		t.Error("want open chain")
	}
	if got, want := zoneNames(o), []string{"A", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("zones = %v, want %v", got, want)
	}
	if o.Zones[2].Key != "P-3" {
		t.Errorf("tail key = %q, want P-3", o.Zones[2].Key)
	}
	if len(o.Links) != 2 || o.Links[1] != (Link{"B", "C", "P-2", "P-3"}) {
		t.Errorf("links = %v", o.Links)
	}
}

func TestCircuitSelfLoop(t *testing.T) {
	o, err := Circuit(its("P-1", "A :: A"))
	if err != nil || !o.Closed || len(o.Zones) != 1 {
		t.Errorf("got %+v, %v", o, err)
	}
}

func TestCircuitIgnoredItems(t *testing.T) {
	o, err := Circuit(its(
		"P-1", "A :: B",
		"P-2", "B :: A",
		"P-3", "no separator",
		"P-4", "X :: Y :: Z",
		"P-5", "a ::b",
		"P-6", " :: B",
		"P-7", "A :: ",
		"P-8", "A ::  Q4'26",
	))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Closed || len(o.Zones) != 2 {
		t.Errorf("got %+v", o)
	}
	if o, err := Circuit(its("P-1", "nothing", "P-2", "a :: b :: c")); err != nil || len(o.Zones) != 0 {
		t.Errorf("no links: got %+v, %v", o, err)
	}
}

func TestCircuitNameParsing(t *testing.T) {

	o, err := Circuit(its(
		"P-1", "x: y: Laundry :: Garage Q4'26",
		"P-2", "Garage :: Laundry Q4'26 extra",
	))
	if err == nil {
		t.Fatalf("token mid-title must stay in the name; got %+v", o)
	}
	if !strings.Contains(err.Error(), `"Laundry Q4'26 extra"`) {
		t.Errorf("err = %v", err)
	}

	o, err = Circuit(its(
		"P-1", "x: y: Laundry :: Garage Q4'26",
		"P-2", "Garage :: Laundry",
	))
	if err != nil || !o.Closed {
		t.Fatalf("got %+v, %v", o, err)
	}
	if got := zoneNames(o); got[0] != "Laundry" || got[1] != "Garage" {
		t.Errorf("zones = %v", got)
	}
}

func TestCircuitErrors(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		want  []string
	}{
		{"duplicate outgoing link", its("P-1", "A :: B", "P-2", "A :: C", "P-3", "B :: A"), []string{`"A"`, "P-1", "P-2", "outgoing"}},
		{"duplicate name", its("P-1", "x: A :: B", "P-2", "A :: B", "P-3", "B :: A"), []string{`"A"`, "P-1", "P-2", "more than one work item"}},
		{"dangling next", its("P-1", "A :: B", "P-2", "B :: Gone"), []string{"P-2", `"Gone"`, "names no work item"}},
		{"ambiguous tail", its("P-1", "A :: B", "P-2", "B", "P-3", "x: B"), []string{"P-1", `"B"`, "P-2", "P-3"}},
		{"fork", its("P-1", "A :: C", "P-2", "B :: C", "P-3", "C :: A"), []string{"P-1", "P-2", `"C"`}},
		{"fork into unlinked tail", its("P-1", "A :: C", "P-2", "B :: C", "P-3", "C"), []string{"P-1", "P-2", `"C"`}},
		{"disjoint loops", its("P-1", "A :: B", "P-2", "B :: A", "P-3", "C :: D", "P-4", "D :: C"), []string{"disjoint", "P-3", "P-4"}},
		{"disjoint chains", its("P-1", "A :: B", "P-2", "B", "P-3", "C :: D", "P-4", "D"), []string{"disjoint", "P-1", "P-3"}},
		{"chain plus loop", its("P-1", "A :: B", "P-2", "B", "P-3", "C :: D", "P-4", "D :: C"), []string{"disjoint", "P-3", "P-4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Circuit(tt.items)
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}
