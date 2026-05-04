package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/internal/ggg"
	"github.com/cptleo92/poe-herald/internal/pobbin"
	"github.com/cptleo92/poe-herald/internal/rag"
	"github.com/jackc/pgx/v5"
)

const (
	chatQuestionOptionName = "question"
	chatSessionTTL         = 15 * time.Minute
	chatPobbMaxBytes       = 120_000
	chatRetrieveK          = 0
	chatRerankTopN         = 0
	discordMessageMax      = 1900

	chatContextSelectID = "chat-context-select"
	chatPobbButtonID    = "chat-pobb-link-btn"
	chatPobbModalID     = "chat-pobb-link-modal"
	chatPobbInputID     = "chat-pobb-link-input"
)

const chatSupplementaryWikiSuffix = `Player context is included in this request.
Treat wiki document snippets as supplementary references. Prioritize direct reasoning from player build data first,
then use wiki snippets to validate mechanics and terminology.`

const chatBuildCoachSuffix = `Build coaching for /chat:
- For dying, survivability, defense, or improving the build: start from concrete facts in the Character snapshot or Path of Building XML. Quote or paraphrase specific visible stats (life, energy shield, resistances, armour, evasion, block, spell suppression chance, etc.).
- Call out notable strengths (e.g. very high spell suppression) and obvious gaps (e.g. undercapped resists, thin life pool) using only what appears in that context. If a stat is not present, say it is unknown—do not invent numbers.
- Give actionable priorities: elemental resistance caps, chaos resistance where relevant, a coherent defensive layer, and adequate life or ES for the content; adapt to PoE1 vs PoE2 from the data or wiki game filter.
- Do not replace build-specific advice with a generic wiki article summary when player context is present. Use wiki documents to clarify mechanics or formulas that support your analysis, not as the main narrative.`

const chatDefenseRetrievalTail = `Focus on Path of Exile character survivability and defensive layers: life and energy shield, elemental and chaos resistances, armour and evasion, spell suppression, block, damage reduction, leech/regen, and practical mitigation tradeoffs. Prefer chunks about defenses and mitigation over death penalties or unrelated mechanics.`

func chatRetrievalQuery(userQuestion string) string {
	q := strings.ToLower(userQuestion)
	hints := []string{
		"dying", "die", "death", "dead", "rip", "surviv", "squish", "tanky", "tank ",
		"defence", "defense", "resist", "mitigation", "defensive", "layer", "layers",
		"oneshot", "one-shot", "one shot", "glass cannon", "ehp", "brick", "hc ",
		"hardcore", "stay alive", "too much damage", "keep dying",
	}
	for _, h := range hints {
		if strings.Contains(q, h) {
			return userQuestion + "\n\n" + chatDefenseRetrievalTail
		}
	}
	return userQuestion
}

func chatFullSystemSuffix(extra string) string {
	s := chatSupplementaryWikiSuffix + "\n\n" + chatBuildCoachSuffix
	if extra != "" {
		s += "\n\n" + extra
	}
	return s
}

func (app *application) handleChatCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if app.ragEngine == nil {
		sendEphemeralChannelMessage(s, i, "Chat is unavailable right now. COHERE_API_KEY is not configured.")
		return
	}

	userID := getUserID(i)
	if userID == "" {
		sendEphemeralChannelMessage(s, i, "Could not resolve your user ID. Try again.")
		return
	}

	question := strings.TrimSpace(chatQuestionFromCommand(i))
	if question == "" {
		sendEphemeralChannelMessage(s, i, "Question is required. Example: `/chat how do I improve my build?`")
		return
	}

	var guildID *string
	if i.GuildID != "" {
		guildID = &i.GuildID
	}

	if err := app.models.ChatSlashSessions.Upsert(userID, question, guildID, chatSessionTTL); err != nil {
		log.Println("chat Upsert session error:", err)
		sendEphemeralChannelMessage(s, i, "Could not save your question. Try again.")
		return
	}

	chars, err := app.models.Characters.GetByUserID(userID)
	if err != nil {
		log.Println("chat list linked chars error:", err)
		sendEphemeralChannelMessage(s, i, "Could not load linked characters. Try again.")
		return
	}

	var rows []discordgo.MessageComponent
	if len(chars) > 0 {
		options := make([]discordgo.SelectMenuOption, 0, len(chars))
		for _, c := range chars {
			game := c.Game
			if game == "" {
				game = ggg.GamePoe1
			}
			options = append(options, discordgo.SelectMenuOption{
				Label:       fmt.Sprintf("%s (Lv. %d %s)", c.Name, c.Level, c.Class),
				Value:       ggg.GamePrefixedName(game, c.Name),
				Description: c.League,
			})
		}
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    chatContextSelectID,
					Placeholder: "Use linked character context",
					Options:     options,
				},
			},
		})
	}

	rows = append(rows, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				CustomID: chatPobbButtonID,
				Label:    "Paste pobb.in link",
				Style:    discordgo.PrimaryButton,
			},
		},
	})

	content := "Question saved. Choose build context next."
	if len(chars) == 0 {
		content += "\nNo linked characters found. Use the pobb.in button, or run `/link-character` later."
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Flags:      discordgo.MessageFlagsEphemeral,
			Components: rows,
		},
	})
	if err != nil {
		log.Println("chat initial response error:", err)
	}
}

func (app *application) handleChatLinkedCharacterSelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if app.ragEngine == nil {
		sendEphemeralInteractionResponse(s, i, "Chat is unavailable right now. COHERE_API_KEY is not configured.")
		return
	}

	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		sendEphemeralInteractionResponse(s, i, "No character selected. Try `/chat` again.")
		return
	}
	game, name := ggg.ParseGamePrefixedName(data.Values[0])
	userID := getUserID(i)

	session, err := app.models.ChatSlashSessions.ConsumeLatest(userID, time.Now().UTC().Add(-chatSessionTTL))
	if err != nil {
		if err == pgx.ErrNoRows {
			sendEphemeralInteractionResponse(s, i, "Your `/chat` question expired. Run `/chat` again.")
			return
		}
		log.Println("chat consume session error:", err)
		sendEphemeralInteractionResponse(s, i, "Could not load your pending question. Try `/chat` again.")
		return
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		log.Println("chat defer linked response error:", err)
		return
	}

	user, err := app.models.Users.GetUser(userID)
	if err != nil {
		log.Println("chat get user error:", err)
		app.chatFollowupStatus(s, i, "Could not load your account. Use `/link-account` and try again.")
		return
	}

	client := ggg.NewClient(user.OauthAccessToken, gggUserAgent)
	detail, err := client.FetchCharacter(name, game)
	if err != nil {
		log.Println("chat fetch character error:", err)
		app.chatFollowupStatus(s, i, "Could not fetch character from Path of Exile. Try `/link-account` again.")
		return
	}

	snapshot := ggg.ToLLMContext(detail)
	retrievalQ := chatRetrievalQuery(session.Question)
	auth := rag.Authoring{
		WikiGameFilter: game,
		SystemSuffix:   chatFullSystemSuffix(""),
		UserPrefix:     "### Character snapshot (filtered Path of Exile API data)\n" + snapshot + "\n\n### Question\n",
		RetrieveK:      rag.PtrInt(chatRetrieveK),
		RerankTopN:     rag.PtrInt(chatRerankTopN),
		RetrievalQuery: retrievalQ,
		RerankMinScore: rag.ChatRerankMinScore,
		LogPipeline:    "chat_linked",
	}

	log.Printf("chat.pipeline discord_user=%s path=linked char=%q game=%q session_q_len=%d session_q_preview=%q retrieval_q_expanded=%v retrieval_q_preview=%q snapshot_len=%d snapshot_preview=%q",
		userID, name, game, len(session.Question), truncateChatLog(session.Question, 400), retrievalQ != session.Question, truncateChatLog(retrievalQ, 400), len(snapshot), truncateChatLog(snapshot, 500))

	answer, err := app.ragEngine.Answer(context.Background(), session.Question, auth)
	if err != nil {
		log.Println("chat rag answer error (linked):", err)
		app.chatFollowupStatus(s, i, "Could not generate answer right now. Try again in a bit.")
		return
	}

	log.Printf("chat.pipeline discord_user=%s path=linked answer_len=%d answer_preview=%q",
		userID, len(answer), truncateChatLog(answer, 500))

	if err := sendDMChunks(s, userID, answer); err != nil {
		log.Println("chat DM error (linked):", err)
		app.chatFollowupStatus(s, i, "I could not DM you the answer. Open DMs with the bot, then rerun `/chat`.")
		return
	}

	app.chatFollowupStatus(s, i, fmt.Sprintf("Answer sent to DM using linked character **%s** (%s).", name, game))
}

func (app *application) handleChatPobbButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: chatPobbModalID,
			Title:    "Paste pobb.in Link",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    chatPobbInputID,
							Label:       "pobb.in URL",
							Style:       discordgo.TextInputShort,
							Placeholder: "https://pobb.in/....",
							Required:    true,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Println("chat open modal error:", err)
	}
}

func (app *application) handleChatPobbModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if app.ragEngine == nil {
		sendEphemeralInteractionResponse(s, i, "Chat is unavailable right now. COHERE_API_KEY is not configured.")
		return
	}

	userID := getUserID(i)
	link := strings.TrimSpace(modalTextValue(i.ModalSubmitData().Components, chatPobbInputID))
	if link == "" {
		sendEphemeralInteractionResponse(s, i, "pobb.in URL is required.")
		return
	}

	session, err := app.models.ChatSlashSessions.ConsumeLatest(userID, time.Now().UTC().Add(-chatSessionTTL))
	if err != nil {
		if err == pgx.ErrNoRows {
			sendEphemeralInteractionResponse(s, i, "Your `/chat` question expired. Run `/chat` again.")
			return
		}
		log.Println("chat consume session error:", err)
		sendEphemeralInteractionResponse(s, i, "Could not load your pending question. Try `/chat` again.")
		return
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		log.Println("chat defer pobb modal error:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pobCtx, err := pobbin.FetchBuildSkillsContext(ctx, link, gggUserAgent, chatPobbMaxBytes)
	if err != nil {
		log.Println("chat pobb fetch error:", err)
		app.chatFollowupStatus(s, i, "Could not read that pobb.in URL. Use a valid `https://pobb.in/...` link.")
		return
	}

	extra := ""
	if pobCtx.Truncated {
		extra = "Path of Building context was truncated to fit token budget. Do not assume omitted sections."
	}
	retrievalQ := chatRetrievalQuery(session.Question)
	auth := rag.Authoring{
		SystemSuffix:   chatFullSystemSuffix(extra),
		UserPrefix:     "### Path of Building XML context (Build, Skills, and Items sections when present)\n" + pobCtx.Content + "\n\n### Question\n",
		RetrieveK:      rag.PtrInt(chatRetrieveK),
		RerankTopN:     rag.PtrInt(chatRerankTopN),
		RetrievalQuery: retrievalQ,
		RerankMinScore: rag.ChatRerankMinScore,
		LogPipeline:    "chat_pobb",
	}

	log.Printf("chat.pipeline discord_user=%s path=pobb url_input_len=%d pob_xml_extract_len=%d truncated=%v session_q_len=%d session_q_preview=%q retrieval_q_expanded=%v retrieval_q_preview=%q pob_preview=%q",
		userID, len(link), len(pobCtx.Content), pobCtx.Truncated, len(session.Question), truncateChatLog(session.Question, 400), retrievalQ != session.Question, truncateChatLog(retrievalQ, 400), truncateChatLog(pobCtx.Content, 500))

	answer, err := app.ragEngine.Answer(context.Background(), session.Question, auth)
	if err != nil {
		log.Println("chat rag answer error (pobb):", err)
		app.chatFollowupStatus(s, i, "Could not generate answer right now. Try again in a bit.")
		return
	}

	log.Printf("chat.pipeline discord_user=%s path=pobb answer_len=%d answer_preview=%q",
		userID, len(answer), truncateChatLog(answer, 500))

	if err := sendDMChunks(s, userID, answer); err != nil {
		log.Println("chat DM error (pobb):", err)
		app.chatFollowupStatus(s, i, "I could not DM you the answer. Open DMs with the bot, then rerun `/chat`.")
		return
	}

	app.chatFollowupStatus(s, i, "Answer sent to DM using pobb.in Build + Skills context.")
}

func chatQuestionFromCommand(i *discordgo.InteractionCreate) string {
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == chatQuestionOptionName {
			return opt.StringValue()
		}
	}
	return ""
}

func modalTextValue(rows []discordgo.MessageComponent, customID string) string {
	for _, row := range rows {
		var comps []discordgo.MessageComponent
		switch v := row.(type) {
		case discordgo.ActionsRow:
			comps = v.Components
		case *discordgo.ActionsRow:
			comps = v.Components
		default:
			continue
		}
		for _, c := range comps {
			switch input := c.(type) {
			case discordgo.TextInput:
				if input.CustomID == customID {
					return input.Value
				}
			case *discordgo.TextInput:
				if input.CustomID == customID {
					return input.Value
				}
			}
		}
	}
	return ""
}

func sendDMChunks(s *discordgo.Session, userID, answer string) error {
	channel, err := s.UserChannelCreate(userID)
	if err != nil {
		return err
	}
	for _, chunk := range splitDiscordMessage(answer, discordMessageMax) {
		if _, err := s.ChannelMessageSend(channel.ID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (app *application) chatFollowupStatus(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: content,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		log.Println("chat followup status error:", err)
	}
}

func truncateChatLog(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
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
