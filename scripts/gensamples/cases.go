package main

import (
	"encoding/json"
	"os"
)

type TestCase struct {
	ID               string   `json:"id"`
	TTSInput         string   `json:"tts_input"`
	RawASR           string   `json:"raw_asr"`
	ExpectedHinted   string   `json:"expected_hinted"`
	ExpectedEntities []string `json:"expected_entities"`
}

func LoadCases(path string) ([]TestCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []TestCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}
