package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"cakefactory/internal/model"
)

// Every container of the factory must be startable and listed in docker-compose.yml: these
// three places (the model, this program, the compose file) are kept in step by this test.

func TestEveryStationNodeCanBeStarted(t *testing.T) {
	for _, n := range model.Nodes {
		if n == model.BT || n == model.MON {
			continue // their own programs
		}
		if _, ok := stations[strings.ToLower(n)]; !ok {
			t.Errorf("node %s has no entry in cmd/station", n)
		}
	}
	for name := range stations {
		found := false
		for _, n := range model.Nodes {
			found = found || strings.ToLower(n) == name
		}
		if !found {
			t.Errorf("cmd/station starts %q, which is no node of the model", name)
		}
	}
}

func compose(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEveryNodeIsAServiceInTheComposeFile(t *testing.T) {
	text := compose(t)
	for _, n := range model.Nodes {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(strings.ToLower(n)) + `:\s*$`).MatchString(text) {
			t.Errorf("node %s has no service %q in docker-compose.yml", n, strings.ToLower(n))
		}
	}
}

func TestComposeStartsEachStationWithItsOwnName(t *testing.T) {
	text := compose(t)
	service := regexp.MustCompile(`(?m)^  ([a-z-]+):\s*$`)
	station := regexp.MustCompile(`STATION: ([a-z-]+)`)
	var current string
	for _, line := range strings.Split(text, "\n") {
		if m := service.FindStringSubmatch(line); m != nil {
			current = m[1]
		}
		if m := station.FindStringSubmatch(line); m != nil && current != "" {
			if m[1] != current {
				t.Errorf("service %s starts station %s", current, m[1])
			}
			if _, ok := stations[m[1]]; !ok {
				t.Errorf("service %s starts unknown station %s", current, m[1])
			}
		}
	}
}

func TestVolumesThatServicesUseAreDeclared(t *testing.T) {
	text := compose(t)
	for _, m := range regexp.MustCompile(`"([a-z-]+-data):/data"`).FindAllStringSubmatch(text, -1) {
		if !strings.Contains(text, "\n  "+m[1]+":") {
			t.Errorf("volume %s is used but not declared", m[1])
		}
	}
}
