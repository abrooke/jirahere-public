package context

import (
	"encoding/json"
	"testing"
)

func TestOutput_UsesExplicitNullForUnfetchedRelations(t *testing.T) {
	data, err := json.Marshal(Output{Level: 0, Item: Item{Key: "PROJ-123"}})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got["parent"] != nil || got["children"] != nil || got["descendants"] != nil || got["descendantFailures"] != nil {
		t.Errorf("unfetched relations = parent %#v children %#v descendants %#v descendantFailures %#v, want explicit null", got["parent"], got["children"], got["descendants"], got["descendantFailures"])
	}
	if _, ok := got["descendantFailures"]; !ok {
		t.Error(`"descendantFailures" key missing from output, want present with explicit null`)
	}
}

func TestOutput_UsesDescendantEnvelope(t *testing.T) {
	descendants := []Descendant{{
		Node:      Relation{Key: "PROJ-125", Type: "Task", Summary: "Nested work", Status: "To Do"},
		Depth:     2,
		ParentKey: "PROJ-124",
	}}
	data, err := json.Marshal(Output{Level: 3, Item: Item{Key: "PROJ-123"}, Descendants: &descendants})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got struct {
		Descendants []struct {
			Node      map[string]any `json:"node"`
			Depth     int            `json:"depth"`
			ParentKey string         `json:"parentKey"`
		} `json:"descendants"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(got.Descendants) != 1 || got.Descendants[0].Depth != 2 || got.Descendants[0].ParentKey != "PROJ-124" {
		t.Fatalf("descendants = %#v, want one envelope with traversal metadata", got.Descendants)
	}
	if len(got.Descendants[0].Node) != 4 || got.Descendants[0].Node["key"] != "PROJ-125" {
		t.Errorf("descendant node = %#v, want exact shallow relation", got.Descendants[0].Node)
	}
}

func TestOutput_UsesEmptyArrayForFetchedEmptyChildren(t *testing.T) {
	children := []Relation{}
	data, err := json.Marshal(Output{Level: 1, Item: Item{Key: "PROJ-123"}, Children: &children})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got struct {
		Children json.RawMessage `json:"children"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if string(got.Children) != "[]" {
		t.Errorf("children = %s, want []", got.Children)
	}
}
