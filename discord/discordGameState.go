package discord

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/denverquane/amongusdiscord/game"
	"github.com/denverquane/amongusdiscord/storage"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

type TrackingChannel struct {
	ChannelID   string `json:"channelID"`
	ChannelName string `json:"channelName"`
}

func (tc TrackingChannel) ToStatusString(sett *storage.GuildSettings) string {
	if tc.ChannelID == "" || tc.ChannelName == "" {
		return sett.LocalizeMessage(&i18n.Message{
			ID:    "discordGameState.ToStatusString.anyVoiceChannel",
			Other: "**No Voice Channel! Use `{{.CommandPrefix}} track`!**",
		},
			map[string]interface{}{
				"CommandPrefix": sett.CommandPrefix,
			})
	} else {
		return tc.ChannelName
	}
}

func (tc TrackingChannel) ToDescString(sett *storage.GuildSettings) string {
	if tc.ChannelID == "" || tc.ChannelName == "" {
		return sett.LocalizeMessage(&i18n.Message{
			ID:    "discordGameState.ToDescString.anyVoiceChannel",
			Other: "**no Voice Channel! Use `{{.CommandPrefix}} track`!**",
		},
			map[string]interface{}{
				"CommandPrefix": sett.CommandPrefix,
			})
	} else {
		return sett.LocalizeMessage(&i18n.Message{
			ID:    "discordGameState.ToDescString.voiceChannelName",
			Other: "the **{{.channelName}}** voice channel!",
		},
			map[string]interface{}{
				"channelName": tc.ChannelName,
			})
	}
}

type DiscordGameState struct {
	GuildID string `json:"guildID"`

	ConnectCode string `json:"connectCode"`

	Linked     bool `json:"linked"`
	Running    bool `json:"running"`
	Subscribed bool `json:"subscribed"`

	MatchID        int64             `json:"matchID"`
	MatchStartUnix int64             `json:"matchStartUnix"`
	Winners        []game.GameWinner `json:"winners"`
	GameResult     game.GameResult   `json:"gameResult"`

	UserData UserDataSet     `json:"userData"`
	Tracking TrackingChannel `json:"tracking"`

	GameStateMsg GameStateMessage `json:"gameStateMessage"`

	AmongUsData game.AmongUsData `json:"amongUsData"`
}

func NewDiscordGameState(guildID string) *DiscordGameState {
	dgs := DiscordGameState{GuildID: guildID}
	dgs.Reset()
	return &dgs
}

func (dgs *DiscordGameState) Reset() {
	//Explicitly does not reset the GuildID!
	dgs.ConnectCode = ""
	dgs.Linked = false
	dgs.Running = false
	dgs.Subscribed = false
	dgs.MatchID = -1
	dgs.MatchStartUnix = -1
	dgs.Winners = []game.GameWinner{}
	dgs.GameResult = -1
	dgs.UserData = map[string]UserData{}
	dgs.Tracking = TrackingChannel{}
	dgs.GameStateMsg = MakeGameStateMessage()
	dgs.AmongUsData = game.NewAmongUsData()
}

func (dgs *DiscordGameState) checkCacheAndAddUser(g *discordgo.Guild, s *discordgo.Session, userID string) (UserData, bool) {
	if g == nil {
		return UserData{}, false
	}
	//check and see if they're cached first
	for _, v := range g.Members {
		if v.User != nil && v.User.ID == userID {
			user := MakeUserDataFromDiscordUser(v.User, v.Nick)
			dgs.UserData[v.User.ID] = user
			return user, true
		}
	}
	mem, err := s.GuildMember(g.ID, userID)
	if err != nil {
		log.Println(err)
		return UserData{}, false
	}
	user := MakeUserDataFromDiscordUser(mem.User, mem.Nick)
	dgs.UserData[mem.User.ID] = user
	return user, true
}

func (dgs *DiscordGameState) clearGameTracking(s *discordgo.Session) {
	//clear the discord User links to underlying player data
	dgs.ClearAllPlayerData()

	//reset all the Tracking channels
	dgs.Tracking = TrackingChannel{}

	dgs.DeleteGameStateMsg(s)
}

func (dgs *DiscordGameState) trackChannel(channelName string, allChannels []*discordgo.Channel, sett *storage.GuildSettings) string {
	for _, c := range allChannels {
		if (strings.ToLower(c.Name) == strings.ToLower(channelName) || c.ID == channelName) && c.Type == 2 {

			dgs.Tracking = TrackingChannel{ChannelName: c.Name, ChannelID: c.ID}

			log.Println(fmt.Sprintf("Now Tracking \"%s\" Voice Channel for Automute!", c.Name))
			return sett.LocalizeMessage(&i18n.Message{
				ID:    "discordGameState.trackChannel.voiceChannelSet",
				Other: "Now Tracking \"{{.channelName}}\" Voice Channel for Automute!",
			},
				map[string]interface{}{
					"channelName": c.Name,
				})
		}
	}
	return sett.LocalizeMessage(&i18n.Message{
		ID:    "discordGameState.trackChannel.voiceChannelNotfound",
		Other: "No channel found by the name {{.channelName}}!\n",
	},
		map[string]interface{}{
			"channelName": channelName,
		})
}

func (dgs *DiscordGameState) ToEmojiEmbedFields(emojis AlivenessEmojis, sett *storage.GuildSettings) []*discordgo.MessageEmbedField {
	unsorted := make([]*discordgo.MessageEmbedField, 12)
	num := 0

	for _, player := range dgs.AmongUsData.PlayerData {
		for _, userData := range dgs.UserData {
			if userData.InGameName == player.Name {
				emoji := emojis[player.IsAlive][player.Color]
				unsorted[player.Color] = &discordgo.MessageEmbedField{
					Name:   fmt.Sprintf("%s", player.Name),
					Value:  fmt.Sprintf("%s <@!%s>", emoji.FormatForInline(), userData.GetID()),
					Inline: true,
				}
				break
			}
		}
		//no player matched; unlinked player
		if unsorted[player.Color] == nil {
			emoji := emojis[player.IsAlive][player.Color]
			unsorted[player.Color] = &discordgo.MessageEmbedField{
				Name: fmt.Sprintf("%s", player.Name),
				Value: fmt.Sprintf("%s **%s**", emoji.FormatForInline(), sett.LocalizeMessage(&i18n.Message{
					ID:    "discordGameState.ToEmojiEmbedFields.Unlinked",
					Other: "Unlinked",
				})),
				Inline: true,
			}
		}
		num++
	}

	sorted := make([]*discordgo.MessageEmbedField, num)
	num = 0
	for i := 0; i < 12; i++ {
		if unsorted[i] != nil {
			sorted[num] = unsorted[i]
			num++
		}
	}
	//balance out the last row of embeds with an extra inline field
	if num == 2 || num == 5 || num == 8 {
		sorted = append(sorted, &discordgo.MessageEmbedField{
			Name:   "\u200b",
			Value:  "\u200b",
			Inline: true,
		})
	}
	return sorted
}
