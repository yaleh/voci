package asr

import "context"

// Result holds the output of a transcription call.
type Result struct{ Text string }

// Options carries all parameters needed to perform a transcription.
type Options struct {
	Key, AudioPath, APIURL, Language, Provider, Model string
	Entities                                          []string
}

// Transcriber is the interface for ASR backends.
type Transcriber interface {
	Transcribe(ctx context.Context, opts Options) (Result, error)
}

// providerTranscriber delegates to the package-level Transcribe function.
type providerTranscriber struct{}

// compile-time interface assertion
var _ Transcriber = providerTranscriber{}

func (p providerTranscriber) Transcribe(ctx context.Context, opts Options) (Result, error) {
	text := Transcribe(ctx, opts.Key, opts.AudioPath, opts.APIURL, opts.Language, opts.Provider, opts.Model, opts.Entities)
	return Result{Text: text}, nil
}

// NewProviderTranscriber returns a Transcriber backed by the built-in provider logic.
func NewProviderTranscriber() Transcriber { return providerTranscriber{} }

// FnFromTranscriber adapts a Transcriber into the daemon.TranscribeFn signature.
// On error the returned function returns an empty string.
func FnFromTranscriber(t Transcriber) func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
	return func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
		r, err := t.Transcribe(ctx, Options{
			Key: key, AudioPath: audioPath, APIURL: apiURL,
			Language: language, Entities: entities,
		})
		if err != nil {
			return ""
		}
		return r.Text
	}
}
