package routing

import (
	"strings"
	"unicode"
)

// Provider catalogs list every model they host. Models that only embed, rerank,
// moderate or transcribe cannot take a chat/tool request, yet detectModality
// files them under "text", so mistral-embed used to be routed for build tasks
// and burned retry attempts. Matching whole id tokens keeps names such as
// "embedded-systems-coder" or "tts" inside a longer word from being excluded.
var nonChatTokens = map[string]bool{
	"embed": true, "embedding": true, "embeddings": true,
	"rerank": true, "reranker": true, "reranking": true,
	"moderation": true, "moderations": true,
	"whisper": true, "tts": true, "transcribe": true, "transcription": true,
	"ocr": true,
	// Image, video and music generators that detectModality files under "text".
	"veo": true, "imagen": true, "banana": true, "image": true, "video": true,
	"lyria": true, "sora": true, "dall": true,
	// Specialist models listed beside the chat models that take none of their requests: a
	// chart-to-table model, a grounding vision model, an image/text embedder, document parsers and
	// a reward scorer. The catalogue calls all of them "text", and a build node was routed to deplot
	// and kosmos-2 first on every run, two attempts spent before one that could write code.
	"deplot": true, "kosmos": true, "nvclip": true, "parse": true, "reward": true,
}

func isNonChatModelID(id string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(id), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, token := range tokens {
		if nonChatTokens[token] {
			return true
		}
	}
	return false
}

// servesChatTasks reports whether a model may be routed for text, coding or
// image-request work. Every selection path and the route diagnostics share it
// so "eligible" means the same thing in each.
func servesChatTasks(model Model) bool {
	// A model detected as an image generator is routed for image tasks by modality;
	// the name list is only for ones that were mistaken for chat models.
	return model.Modality == "image" || !isNonChatModelID(model.ID)
}
