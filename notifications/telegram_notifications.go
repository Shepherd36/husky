// SPDX-License-Identifier: ice License 1.0

package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"text/template"
	stdlibtime "time"

	"github.com/pkg/errors"

	"github.com/ice-blockchain/eskimo/users"
	storage "github.com/ice-blockchain/wintr/connectors/storage/v2"
	"github.com/ice-blockchain/wintr/log"
	"github.com/ice-blockchain/wintr/notifications/telegram"
	"github.com/ice-blockchain/wintr/time"
)

type (
	telegramNotificationTemplate struct {
		body, inviteText, altBody *template.Template
		Body                      string               `json:"body"`       //nolint:revive // That's intended.
		AltBody                   string               `json:"altBody"`    //nolint:revive // That's intended.
		InviteText                string               `json:"inviteText"` //nolint:revive // That's intended.
		ButtonText                []string             `json:"buttonText"`
		buttonText                []*template.Template //nolint:revive // That's intended.
	}
)

func (t *telegramNotificationTemplate) getButtonText(data any, ix int) string { //nolint:unparam // .
	if ix > len(t.buttonText)-1 {
		return ""
	}
	if data == nil {
		return t.ButtonText[ix]
	}
	bf := new(bytes.Buffer)
	log.Panic(errors.Wrapf(t.buttonText[ix].Execute(bf, data), "failed to execute button text 1 template for data:%#v", data))

	return bf.String()
}

func (t *telegramNotificationTemplate) getBody(data any) string {
	if data == nil {
		return t.Body
	}
	bf := new(bytes.Buffer)
	log.Panic(errors.Wrapf(t.body.Execute(bf, data), "failed to execute body template for data:%#v", data))

	return bf.String()
}

func (t *telegramNotificationTemplate) getAltBody(data any) string {
	if data == nil {
		return t.AltBody
	}
	bf := new(bytes.Buffer)
	log.Panic(errors.Wrapf(t.altBody.Execute(bf, data), "failed to execute altBody template for data:%#v", data))

	return bf.String()
}

func (t *telegramNotificationTemplate) getInviteText(data any) string {
	if t.inviteText == nil {
		return ""
	}
	if data == nil {
		return t.InviteText
	}
	bf := new(bytes.Buffer)
	if err := t.inviteText.Execute(bf, data); err != nil {
		return ""
	}

	return bf.String()
}

func loadTelegramNotificationTranslationTemplates() { //nolint:funlen,revive,gocognit // .
	const totalLanguages = 50
	allTelegramNotificationTemplates = make(map[NotificationType]map[languageCode]*telegramNotificationTemplate, len(AllTelegramNotificationTypes))
	for _, notificationType := range AllTelegramNotificationTypes {
		content, fErr := translations.ReadFile(fmt.Sprintf("translations/telegram/%v.txt", notificationType))
		if fErr != nil {
			panic(fErr)
		}
		allTelegramNotificationTemplates[notificationType] = make(map[languageCode]*telegramNotificationTemplate, totalLanguages)
		var translations map[string]*struct {
			Body       string   `json:"body"`
			AltBody    string   `json:"altBody"`
			InviteText string   `json:"inviteText"`
			ButtonText []string `json:"buttonText"`
		}
		err := json.Unmarshal(content, &translations)
		if err != nil {
			panic(err)
		}
		for language, data := range translations {
			var tmpl telegramNotificationTemplate
			tmpl.Body = data.Body
			tmpl.AltBody = data.AltBody
			if notificationType == InviteFriendNotificationType {
				tmpl.InviteText = data.InviteText
				tmpl.inviteText = template.Must(template.New(fmt.Sprintf("telegram_%v_%v_invite_text", notificationType, language)).Parse(data.InviteText))
			}
			if tmpl.AltBody != "" {
				tmpl.altBody = template.Must(template.New(fmt.Sprintf("push_%v_%v_alt_body", notificationType, language)).Parse(data.AltBody))
			}
			tmpl.ButtonText = data.ButtonText
			for ix := range data.ButtonText {
				tmpl.buttonText = append(tmpl.buttonText, template.Must(template.New(fmt.Sprintf("telegram_%v_%v_button_text_%v", notificationType, language, ix)).Parse(data.ButtonText[ix]))) //nolint:lll // .
			}
			tmpl.body = template.Must(template.New(fmt.Sprintf("telegram_%v_%v_body", notificationType, language)).Parse(data.Body))
			allTelegramNotificationTemplates[notificationType][language] = &tmpl
		}
	}
}

//nolint:exhaustive // We know what cases need to be handled only.
func getTelegramDeeplink(nt NotificationType, cfg *config, username, inviteText string) string {
	switch nt {
	case MiningExtendNotificationType, MiningEndingSoonNotificationType, MiningExpiredNotificationType, MiningNotActiveNotificationType:
		return cfg.WebAppLink
	case InviteFriendNotificationType:
		return fmt.Sprintf("%[1]v?url=%[2]v/@%[3]v&text=%[4]v", cfg.InviteURL, url.QueryEscape(cfg.WebSiteURL), url.QueryEscape(username), url.QueryEscape(inviteText)) //nolint:lll // .
	case CoinBadgeUnlockedNotificationType, LevelBadgeUnlockedNotificationType, SocialBadgeUnlockedNotificationType:
		return fmt.Sprintf("%v?startapp=goto_profile_badges", cfg.WebAppLink)
	case LevelChangedNotificationType:
		return fmt.Sprintf("%v?startapp=goto_profile", cfg.WebAppLink)
	case ReplyNotificationType:
		return cfg.WebAppLink
	case NewReferralNotificationType:
		return fmt.Sprintf("%v?startapp=goto_team", cfg.WebAppLink)
	default:
		log.Panic(fmt.Sprintf("wrong notification type:%v", nt))
	}

	return ""
}

func prepareTelegramButtonsForSocialNotificationType(cfg *config, buttonTexts []string) []struct {
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
} {
	urls := getSocialsMapURL(cfg)
	if len(urls) != len(buttonTexts) {
		log.Error(errors.New("socials cfg/translation misconfiguration"))

		return nil
	}
	res := make([]struct {
		Text string `json:"text,omitempty"`
		URL  string `json:"url,omitempty"`
	}, 0, len(buttonTexts))
	for ix, text := range buttonTexts {
		res = append(res, struct {
			Text string `json:"text,omitempty"`
			URL  string `json:"url,omitempty"`
		}{
			Text: text,
			URL:  urls[ix],
		})
	}

	return res
}

func getSocialsMapURL(cfg *config) map[int]string {
	if len(cfg.Socials) == 0 {
		log.Panic("no urls for socials")
	}
	urls := make(map[int]string, len(cfg.Socials))
	for ix := range cfg.Socials {
		urls[ix] = cfg.Socials[ix].Link
	}

	return urls
}

func (s *Scheduler) startGetUpdatesTelegramLongPolling(ctx context.Context) {
	ticker := stdlibtime.NewTicker(30 * stdlibtime.Second) //nolint:gomnd,mnd // .
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			wg := new(sync.WaitGroup)
			wg.Add(len(s.cfg.TelegramBots))
			go func() {
				defer wg.Done()
				reqCtx, cancel := context.WithTimeout(ctx, 1*stdlibtime.Minute)
				if err := s.getTelegramLongPollingUpdates(reqCtx); err != nil {
					log.Error(errors.Wrap(err, "failed to get updates long polling"))
				}
				cancel()
			}()
			wg.Wait()
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) getTelegramLongPollingUpdates(ctx context.Context) (err error) {
	for _, bot := range s.cfg.TelegramBots {
		nextOffset := int64(0)
		for {
			upd, gErr := s.telegramNotificationsClient.GetUpdates(ctx, &telegram.GetUpdatesArg{
				BotToken:       bot.BotToken,
				AllowedUpdates: []string{"message", "callback_query"},
				Limit:          telegramLongPollingLimit,
				Offset:         nextOffset,
			})
			if gErr != nil {
				return errors.Wrapf(gErr, "can't get updates for offset: %v", nextOffset)
			}
			if len(upd) == 0 {
				break
			}
			nextOffset, err = s.handleTelegramUpdates(ctx, upd)
			if err != nil {
				return errors.Wrapf(err, "can't handle telegram updates for:%v", upd)
			}
		}
	}

	return nil
}

//nolint:funlen // .
func (s *Scheduler) handleTelegramUpdates(ctx context.Context, updates []*telegram.Update) (nextOffset int64, err error) {
	var (
		maxUpdateID    = int64(0)
		now            = time.Now()
		scheduled      = make([]*scheduledNotification, 0, len(updates))
		uniquenessTime = fmt.Sprintf("%v:%02d:%02d %02d:%02d:%02d", now.Year(), int(now.Month()), now.Day(), now.Hour(), now.Minute(), now.Second())
	)
	userInfoMap, err := s.getReplyUserInfo(ctx, updates)
	if err != nil {
		return 0, errors.Wrapf(err, "can't get user ids by telegram user ids for:%#v", updates)
	}
	if len(userInfoMap) == 0 {
		return 0, nil
	}
	for _, upd := range updates {
		if upd.Message.From.IsBot {
			log.Warn("The message are sent through the bot", upd)

			continue
		}
		if upd.UpdateID > maxUpdateID {
			maxUpdateID = upd.UpdateID
		}
		idStr := strconv.FormatInt(upd.Message.From.ID, 10)
		if _, ok := userInfoMap[idStr]; !ok {
			log.Warn("no such telegram user id", upd.Message.From.ID)

			continue
		}
		scheduled = append(scheduled, &scheduledNotification{
			ScheduledAt:  now,
			ScheduledFor: time.New(now.Add(1 * stdlibtime.Minute)),
			Data: &users.JSON{
				"Username": upd.Message.From.Username,
			},
			Language:                 userInfoMap[idStr].Language,
			UserID:                   userInfoMap[idStr].UserID,
			Uniqueness:               fmt.Sprintf("%v_%v_%v", ReplyNotificationType, upd.Message.MessageID, uniquenessTime),
			NotificationType:         string(ReplyNotificationType),
			NotificationChannel:      string(TelegramNotificationChannel),
			NotificationChannelValue: strconv.FormatInt(upd.Message.MessageID, 10),
		})
	}
	if iErr := insertScheduledNotifications(ctx, s.db, scheduled); iErr != nil {
		return 0, errors.Wrapf(iErr, "can't insert scheduled notifications for:%#v", updates)
	}

	return maxUpdateID + 1, nil
}

func (s *Scheduler) getReplyUserInfo(ctx context.Context, updates []*telegram.Update) (res map[telegramUserID]*telegramUserInfo, err error) {
	if len(updates) == 0 {
		return
	}
	telegramUserIDs := make([]telegramUserID, 0, len(updates))
	for _, upd := range updates {
		telegramUserIDs = append(telegramUserIDs, strconv.FormatInt(upd.Message.From.ID, 10))
	}
	sql := `SELECT 
				user_id,
				COALESCE(telegram_user_id, '') AS telegram_user_id,
				language
			FROM users 
			WHERE telegram_user_id = ANY($1)`
	result, err := storage.Select[telegramUserInfo](ctx, s.db, sql, telegramUserIDs)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get user ids for telegram user ids:%#v", telegramUserIDs)
	}
	if result == nil {
		return
	}
	res = make(map[telegramUserID]*telegramUserInfo, len(result))
	for _, val := range result {
		res[val.TelegramUserID] = val
	}

	return
}
