package command

func containsLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

func swapLabel(source []string, hasSwap bool, old, newLabel string) (newLabels, other []string) {
	if !hasSwap {
		return append([]string(nil), source...), nil
	}
	other = make([]string, 0, len(source))
	newLabels = make([]string, 0, len(source))
	inserted := false
	for _, l := range source {
		switch l {
		case old, newLabel:
			if !inserted {
				newLabels = append(newLabels, newLabel)
				inserted = true
			}
		default:
			other = append(other, l)
			newLabels = append(newLabels, l)
		}
	}
	if !inserted {
		newLabels = append(newLabels, newLabel)
	}
	return newLabels, other
}
