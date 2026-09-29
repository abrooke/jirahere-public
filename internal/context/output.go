package context

type Relation struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

type Item struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Summary     string   `json:"summary"`
	Status      string   `json:"status"`
	Labels      []string `json:"labels"`
	Description string   `json:"description"`
}

type Descendant struct {
	Node      Relation `json:"node"`
	Depth     int      `json:"depth"`
	ParentKey string   `json:"parentKey"`
}

type DescendantFailure struct {
	Key       string `json:"key"`
	ParentKey string `json:"parentKey"`
	Depth     int    `json:"depth"`
	Reason    string `json:"reason"`
}

type Output struct {
	Level              int                  `json:"level"`
	Item               Item                 `json:"item"`
	Parent             *Relation            `json:"parent"`
	Ancestors          *[]Relation          `json:"ancestors"`
	Children           *[]Relation          `json:"children"`
	Descendants        *[]Descendant        `json:"descendants"`
	DescendantFailures *[]DescendantFailure `json:"descendantFailures"`
}

var MarkdownSectionOrder = []string{"Labels", "Description", "Parent", "Ancestors", "Children"}
