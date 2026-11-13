package main

import (
	"net/http/httptest"
	"testing"
)

func ratingPtr(n float64) *float64 { return &n }
func TestRatingFiltersAndUnratedTitles(t *testing.T) {
	for _, tc := range []struct {
		rating    string
		low, high float64
		want      bool
	}{
		{"7.5", 7, 8, true}, {"6.9", 7, 10, false}, {"8.1", 7, 8, false}, {"", 9, 10, true}, {"0", 9, 10, true}, {"N/A", 9, 10, true}, {"NaN", 9, 10, true}, {"", 0, 9, false}, {"7.5", 8, 7, true},
	} {
		if got := movieMatches(Movie{Rating: tc.rating}, Filters{RatingMin: ratingPtr(tc.low), RatingMax: ratingPtr(tc.high)}); got != tc.want {
			t.Errorf("rating %q bounds %.1f..%.1f: %v", tc.rating, tc.low, tc.high, got)
		}
	}
	if !movieMatches(Movie{Rating: "2"}, Filters{}) {
		t.Fatal("legacy filters changed")
	}
	f := filtersFromQuery(httptest.NewRequest("GET", "/api/movies?ratingMin=7.5&ratingMax=9", nil))
	if f.RatingMin == nil || *f.RatingMin != 7.5 || f.RatingMax == nil || *f.RatingMax != 9 {
		t.Fatal("library bounds dropped")
	}
	f, err := cleanDefaultFilters(M{"ratingMin": 9, "ratingMax": 7})
	if err != nil || *f.RatingMin != 7 || *f.RatingMax != 9 {
		t.Fatal("saved bounds not normalized")
	}
	if _, err = cleanDefaultFilters(M{"ratingMin": 11}); err == nil {
		t.Fatal("invalid rating accepted")
	}
	// Ratings are selection policy; upstream metadata must stay unmodified.
	movie := Movie{Rating: ""}
	movieMatches(movie, f)
	if movie.Rating != "" {
		t.Fatal("unrated metadata rewritten")
	}
}
