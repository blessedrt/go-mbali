// Copyright 2018 The go-mbali Authors
// This file is part of the go-mbali library.
//
// The go-mbali library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-mbali library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-mbali library. If not, see <http://www.gnu.org/licenses/>.

package accounts

import (
	"testing"
)

func TestURLParsing(t *testing.T) {
	url, err := parseURL("https://mbali.org")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if url.Scheme != "https" {
		t.Errorf("expected: %v, got: %v", "https", url.Scheme)
	}
	if url.Path != "mbali.org" {
		t.Errorf("expected: %v, got: %v", "mbali.org", url.Path)
	}

	for _, u := range []string{"mbali.org", ""} {
		if _, err = parseURL(u); err == nil {
			t.Errorf("input %v, expected err, got: nil", u)
		}
	}
}

func TestURLString(t *testing.T) {
	url := URL{Scheme: "https", Path: "mbali.org"}
	if url.String() != "https://mbali.org" {
		t.Errorf("expected: %v, got: %v", "https://mbali.org", url.String())
	}

	url = URL{Scheme: "", Path: "mbali.org"}
	if url.String() != "mbali.org" {
		t.Errorf("expected: %v, got: %v", "mbali.org", url.String())
	}
}

func TestURLMarshalJSON(t *testing.T) {
	url := URL{Scheme: "https", Path: "mbali.org"}
	json, err := url.MarshalJSON()
	if err != nil {
		t.Errorf("unexpcted error: %v", err)
	}
	if string(json) != "\"https://mbali.org\"" {
		t.Errorf("expected: %v, got: %v", "\"https://mbali.org\"", string(json))
	}
}

func TestURLUnmarshalJSON(t *testing.T) {
	url := &URL{}
	err := url.UnmarshalJSON([]byte("\"https://mbali.org\""))
	if err != nil {
		t.Errorf("unexpcted error: %v", err)
	}
	if url.Scheme != "https" {
		t.Errorf("expected: %v, got: %v", "https", url.Scheme)
	}
	if url.Path != "mbali.org" {
		t.Errorf("expected: %v, got: %v", "https", url.Path)
	}
}

func TestURLComparison(t *testing.T) {
	tests := []struct {
		urlA   URL
		urlB   URL
		expect int
	}{
		{URL{"https", "mbali.org"}, URL{"https", "mbali.org"}, 0},
		{URL{"http", "mbali.org"}, URL{"https", "mbali.org"}, -1},
		{URL{"https", "mbali.org/a"}, URL{"https", "mbali.org"}, 1},
		{URL{"https", "abc.org"}, URL{"https", "mbali.org"}, -1},
	}

	for i, tt := range tests {
		result := tt.urlA.Cmp(tt.urlB)
		if result != tt.expect {
			t.Errorf("test %d: cmp mismatch: expected: %d, got: %d", i, tt.expect, result)
		}
	}
}
