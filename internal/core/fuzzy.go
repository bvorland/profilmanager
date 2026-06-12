package core

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const maxFuzzyInputRunes = 100

// SuggestNames returns up to 3 profile names within edit distance maxDist
// of input, sorted by distance ascending. It returns nil if no suggestions.
func SuggestNames(input string, maxDist int) []string {
	if maxDist < 0 {
		return nil
	}
	input = capFuzzyInput(input)
	names, err := profileNames()
	if err != nil || len(names) == 0 {
		return nil
	}

	type suggestion struct {
		name string
		dist int
	}
	var suggestions []suggestion
	inputFold := strings.ToLower(input)
	for _, name := range names {
		dist := levenshtein(inputFold, strings.ToLower(name))
		if dist <= maxDist {
			suggestions = append(suggestions, suggestion{name: name, dist: dist})
		}
	}
	if len(suggestions) == 0 {
		return nil
	}
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].dist != suggestions[j].dist {
			return suggestions[i].dist < suggestions[j].dist
		}
		return strings.ToLower(suggestions[i].name) < strings.ToLower(suggestions[j].name)
	})
	if len(suggestions) > 3 {
		suggestions = suggestions[:3]
	}

	out := make([]string, len(suggestions))
	for i, s := range suggestions {
		out[i] = s.name
	}
	return out
}

func capFuzzyInput(input string) string {
	runes := []rune(input)
	if len(runes) <= maxFuzzyInputRunes {
		return input
	}
	fmt.Fprintf(os.Stderr, "profilmanager: fuzzy input longer than %d characters; truncating\n", maxFuzzyInputRunes)
	return string(runes[:maxFuzzyInputRunes])
}

func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}

	for i, ac := range ar {
		curr[0] = i + 1
		for j, bc := range br {
			cost := 0
			if ac != bc {
				cost = 1
			}
			curr[j+1] = min3(
				curr[j]+1,
				prev[j+1]+1,
				prev[j]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
