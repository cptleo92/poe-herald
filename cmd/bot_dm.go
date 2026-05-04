package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
	"github.com/cptleo92/poe-herald/internal/ggg"
	"github.com/cptleo92/poe-herald/internal/rag"
	"github.com/jackc/pgx/v5"
)

const discordMessageMax = 1900

const (
	dmNoAccountHint = `Player context: this Discord user has not linked a Path of Exile account. If they want personalized help using their real character gear and tree, mention they can run /link-account and then /link-character on the bot. You may still answer mechanics from the wiki documents.`

	dmNoCharactersHint = `Player context: this user has linked a Path of Exile account but has not linked a character. For build-specific advice tied to their gear/passives, mention they can use /link-character. You may still answer mechanics from the wiki documents.`

	// dmSnapshotHint = `Player context: the user message includes a live Character snapshot section (filtered official API JSON). Use it for build-specific reasoning together with the wiki documents.`

	// dmFetchFailFooter = "\n\n_(Character snapshot could not be loaded from Path of Exile — token may have expired or the API rate-limited. Try `/link-account` again. Answer above uses wiki only.)_"
)

func (app *application) handleDirectMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	if m.GuildID != "" {
		return
	}
	if app.ragEngine == nil {
		return
	}

	q := strings.TrimSpace(m.Content)
	if q == "" {
		return
	}

	auth, footer, dmPath := app.dmRAGAuthoring(m.Author.ID, q)
	log.Printf("dm_rag discord_user=%s path=%s q_len=%d wiki_game_filter=%q user_prefix_bytes=%d system_suffix_bytes=%d footer=%t",
		m.Author.ID, dmPath, len(q), auth.WikiGameFilter, len(auth.UserPrefix), len(auth.SystemSuffix), footer != "")
	answer, err := app.ragEngine.Answer(context.Background(), q, auth)
	if err != nil {
		log.Println("RAG Answer error:", err)
		_, _ = s.ChannelMessageSend(m.ChannelID, "Something went wrong processing your question. Try again later.")
		return
	}

	answer += footer

	for _, chunk := range splitDiscordMessage(answer, discordMessageMax) {
		if _, err := s.ChannelMessageSend(m.ChannelID, chunk); err != nil {
			log.Println("DM send error:", err)
			return
		}
	}
}

// dmRAGAuthoring builds wiki filter, system suffix, and optional user JSON prefix for DM RAG.
// The returned string is a short path label for server logs (dm_rag path=…).
func (app *application) dmRAGAuthoring(discordUserID, question string) (auth rag.Authoring, footer string, path string) {
	_, err := app.models.Users.GetUser(discordUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			auth.SystemSuffix = dmNoAccountHint
			return auth, footer, "no_poe_account"
		}
		log.Printf("dmRAGAuthoring GetUser: %v", err)
		return auth, footer, "get_user_error"
	}

	chars, err := app.models.Characters.GetByUserID(discordUserID)
	if err != nil {
		log.Printf("dmRAGAuthoring GetByUserID: %v", err)
		return auth, footer, "list_chars_error"
	}

	if len(chars) == 0 {
		auth.SystemSuffix = dmNoCharactersHint
		return auth, footer, "no_linked_characters"
	}

	resolved, resolvePath := resolveLinkedCharacterForQuestion(question, chars)
	if resolved == nil {
		names := make([]string, 0, len(chars))
		for _, c := range chars {
			names = append(names, fmt.Sprintf("%s(%s)", c.Name, effectiveGame(c.Game)))
		}
		log.Printf("dmRAGAuthoring ambiguous_resolve discord_user=%s reason=%s linked=%v",
			discordUserID, resolvePath, names)
		auth.SystemSuffix = rosterAndDisambiguationSuffix(chars)
		return auth, footer, "ambiguous_roster"
	}

	game := resolved.Game
	if game == "" {
		game = ggg.GamePoe1
	}
	auth.WikiGameFilter = game

	// --- Character API snapshot → LLM (disabled while moving to PoB export / richer build context) ---
	// client := ggg.NewClient(user.OauthAccessToken, gggUserAgent)
	// detail, err := client.FetchCharacter(resolved.Name, game)
	// if err != nil {
	// 	log.Printf("dmRAGAuthoring FetchCharacter %s (%s): %v", resolved.Name, game, err)
	// 	footer = dmFetchFailFooter
	// 	return auth, footer, "fetch_character_error"
	// }
	// auth.SystemSuffix = dmSnapshotHint
	// snap := ggg.ToLLMContext(detail)
	// auth.UserPrefix = "### Character snapshot (filtered Path of Exile API data)\n" + snap + "\n\n### Question\n"
	// passiveHashes := 0
	// if detail.Passives != nil {
	// 	passiveHashes = len(detail.Passives.Hashes)
	// }
	// log.Printf("dmRAGAuthoring snapshot_ok char=%s game=%s json_bytes=%d equipment=%d jewels=%d passive_hashes=%d inventory=%d",
	// 	resolved.Name, game, len(snap), len(detail.Equipment), len(detail.Jewels), passiveHashes, len(detail.Inventory))
	// return auth, footer, "snapshot_"+resolvePath
	log.Printf("dmRAGAuthoring snapshot_disabled char=%s game=%s resolve=%s", resolved.Name, game, resolvePath)
	return auth, footer, "snapshot_disabled_" + resolvePath
}

func effectiveGame(g string) string {
	if g == "" {
		return ggg.GamePoe1
	}
	return g
}

// resolveLinkedCharacterForQuestion picks which linked character applies to this message.
// path for logs: single_linked | name_match_unique | name_match_ambiguous | name_match_none.
func resolveLinkedCharacterForQuestion(q string, chars []database.Character) (resolved *database.Character, path string) {
	if len(chars) == 0 {
		return nil, "none"
	}
	if len(chars) == 1 {
		return &chars[0], "single_linked"
	}
	qLower := strings.ToLower(q)
	var hits []database.Character
	for i := range chars {
		c := &chars[i]
		nameLower := strings.ToLower(c.Name)
		if nameLower != "" && strings.Contains(qLower, nameLower) {
			hits = append(hits, *c)
		}
	}
	if len(hits) == 1 {
		return &hits[0], "name_match_unique"
	}
	if len(hits) > 1 {
		return nil, "name_match_ambiguous"
	}
	return nil, "name_match_none"
}

func rosterAndDisambiguationSuffix(chars []database.Character) string {
	var b strings.Builder
	b.WriteString("Linked characters:\n")
	for _, c := range chars {
		g := c.Game
		if g == "" {
			g = ggg.GamePoe1
		}
		fmt.Fprintf(&b, "- **%s** (%s) — Level %d %s, league %s\n", c.Name, g, c.Level, c.Class, c.League)
	}
	b.WriteString(`
If the player asks for build- or character-specific advice and their message does not clearly name one of the characters above (by in-game character name), ask which character they mean before giving build-specific recommendations.
For pure mechanics questions that only need the wiki documents, you do not need a character snapshot.`)
	return b.String()
}

func splitDiscordMessage(s string, maxRunes int) []string {
	if s == "" {
		return nil
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return []string{s}
	}
	var out []string
	for i := 0; i < len(r); i += maxRunes {
		end := i + maxRunes
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}
