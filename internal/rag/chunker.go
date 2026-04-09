package rag

import (
	"strings"
	"unicode"
)

// In RAG, we cannot feed entire wiki pages or a full 40-minute YouTube transcript
// into the embedding model or the LLM all at once. Doing so:
// 1. Surpasses context window (token limit) boundaries.
// 2. Dilutes the "semantic meaning" of the vector. (A 10,000 word page on "Bleed" might have
//    only 1 paragraph on "Bleed Duration". We want a tight chunk focused on Duration).
//
// To solve this, we "chunk" the text into smaller pieces.
// We also use "Overlap" — meaning Chunk B starts slightly before Chunk A ends.
// Overlap prevents important words or sentences from being cut exactly in half between chunks.

const (
	// These are rough character estimates. In production setups, people often use
	// specialized "Tokenizer" packages (like tiktoken) to split exactly by LLM tokens.
	// For simplicity, we are splitting by words, using general approximations:
	// 1 token ≈ 0.75 words.  If we want roughly ~500 token chunks:
	TargetChunkWords = 400
	OverlapWords     = 50
)

// DocumentChunk represents a single piece of a larger document.
type DocumentChunk struct {
	Text       string
	StartIndex int // The word index where this chunk started in the original doc
}

// ChunkText takes a massive string (like a full Wiki page) and splits it into
// overlapping DocumentChunks based on word counts.
func ChunkText(content string) []DocumentChunk {
	// 1. Clean the text broadly (remove excessive whitespace, newlines)
	cleaned := cleanText(content)

	// 2. Split the content into an array of words
	// using strings.Fields, which splits nicely on all whitespace (spaces, tabs, newlines).
	words := strings.Fields(cleaned)

	if len(words) == 0 {
		return nil
	}

	var chunks []DocumentChunk

	// 3. Iterate through the array of words, taking slices
	for i := 0; i < len(words); i += (TargetChunkWords - OverlapWords) {

		// Determine where this chunk should end
		end := min(i+TargetChunkWords, len(words))

		// Grab the slice of words
		chunkWords := words[i:end]

		// Join them back into a single string
		chunkText := strings.Join(chunkWords, " ")

		chunks = append(chunks, DocumentChunk{
			Text:       chunkText,
			StartIndex: i,
		})

		// If we've hit the end of the text, break out of the loop
		if end == len(words) {
			break
		}
	}

	return chunks
}

// cleanText removes excessive formatting and unhelpful whitespace
// so that our chunks are dense with actual information rather than spaces.
func cleanText(raw string) string {
	// Replace standard newlines or tab characters with a single space.
	// strings.Map explores a string character by character.
	mapped := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, raw)

	// In the real world, you might also strip out HTML tags here or markdown syntax
	// but for now, ensuring clean spaces is priority #1.

	// Because we replaced tabs/newlines with spaces, we might have multiple spaces in a row (e.g. "   ").
	// strings.Fields handles ignoring these automatically.
	return mapped
}
