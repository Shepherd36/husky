// SPDX-License-Identifier: ice License 1.0

package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	"github.com/ice-blockchain/eskimo/users"
	storage "github.com/ice-blockchain/wintr/connectors/storage/v2"
	"github.com/ice-blockchain/wintr/log"
	"github.com/ice-blockchain/wintr/notifications/push"
	"github.com/ice-blockchain/wintr/time"
)

type (
	pushNotificationTemplate struct {
		title, body *template.Template
		Title       string `json:"title"` //nolint:revive // That's intended.
		Body        string `json:"body"`  //nolint:revive // That's intended.
	}
)

func (t *pushNotificationTemplate) getTitle(data any) string {
	if data == nil {
		return t.Title
	}
	bf := new(bytes.Buffer)
	log.Panic(errors.Wrapf(t.title.Execute(bf, data), "failed to execute title template for data:%#v", data))

	return bf.String()
}

func (t *pushNotificationTemplate) getBody(data any) string {
	if data == nil {
		return t.Body
	}
	bf := new(bytes.Buffer)
	log.Panic(errors.Wrapf(t.body.Execute(bf, data), "failed to execute body template for data:%#v", data))

	return bf.String()
}

func loadPushNotificationTranslationTemplates() {
	const totalLanguages = 50
	allPushNotificationTemplates = make(map[NotificationType]map[languageCode]*pushNotificationTemplate, len(AllNotificationTypes))
	for _, notificationType := range AllNotificationTypes {
		content, fErr := translations.ReadFile(fmt.Sprintf("translations/push/%v.txt", notificationType))
		if fErr != nil {
			panic(fErr)
		}
		allPushNotificationTemplates[notificationType] = make(map[languageCode]*pushNotificationTemplate, totalLanguages)
		var translations map[string]*struct {
			Body  string `json:"body"`
			Title string `json:"title"`
		}
		err := json.Unmarshal(content, &translations)
		if err != nil {
			panic(err)
		}
		for language, data := range translations {
			var tmpl pushNotificationTemplate
			tmpl.Body = data.Body
			tmpl.Title = data.Title
			tmpl.title = template.Must(template.New(fmt.Sprintf("push_%v_%v_title", notificationType, language)).Parse(data.Title))
			tmpl.body = template.Must(template.New(fmt.Sprintf("push_%v_%v_body", notificationType, language)).Parse(data.Body))
			allPushNotificationTemplates[notificationType][language] = &tmpl
		}
	}
}

type (
	pushNotificationTokens struct {
		PushNotificationTokens        *users.Enum[push.DeviceToken]
		Language, UserID              string
		TelegramUserID, TelegramBotID string
	}
)

func (r *repository) getPushNotificationTokens(
	ctx context.Context, domain NotificationDomain, userID string,
) (*pushNotificationTokens, error) {
	if ctx.Err() != nil {
		return nil, errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	sql := fmt.Sprintf(`SELECT array_agg(dm.push_notification_token) filter (where dm.push_notification_token is not null)  AS push_notification_tokens, 
							   u.language,
							   u.user_id,
							   COALESCE(u.telegram_user_id, '') AS telegram_user_id,
							   COALESCE(u.telegram_bot_id, '') AS telegram_bot_id
						FROM users u
							 LEFT JOIN device_metadata dm
									ON ( u.disabled_push_notification_domains IS NULL 
										OR NOT (u.disabled_push_notification_domains && ARRAY['%[1]v','%[2]v'] )
								   	   )
								   AND dm.user_id = u.user_id
								   AND dm.push_notification_token IS NOT NULL 
								   AND dm.push_notification_token != ''
						WHERE u.user_id = $1
						GROUP BY u.user_id`, domain, AllNotificationDomain)
	resp, err := storage.Get[pushNotificationTokens](ctx, r.db, sql, userID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select for push notification tokens for `%v`, userID:%#v", domain, userID)
	}

	return resp, nil
}

type (
	sentNotificationPK struct {
		UserID                   string              `json:"userId,omitempty" example:"edfd8c02-75e0-4687-9ac2-1ce4723865c4"`
		Uniqueness               string              `json:"uniqueness,omitempty" example:"anything"`
		NotificationType         NotificationType    `json:"notificationType,omitempty" example:"adoption_changed"`
		NotificationChannel      NotificationChannel `json:"notificationChannel,omitempty" example:"email"`
		NotificationChannelValue string              `json:"notificationChannelValue,omitempty" example:"jdoe@example.com"`
	}
	sentNotification struct {
		SentAt   *time.Time `json:"sentAt,omitempty" example:"2022-01-03T16:20:52.156534Z"`
		Language string     `json:"language,omitempty" example:"en"`
		sentNotificationPK
	}
	pushNotification struct {
		pn *push.Notification[push.DeviceToken]
		sn *sentNotification
	}
)

func (r *repository) sendPushNotification(ctx context.Context, pn *pushNotification) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	if err := r.insertSentNotification(ctx, pn.sn); err != nil {
		return errors.Wrapf(err, "failed to insert %#v", pn.sn)
	}
	responder := make(chan error, 1)
	defer close(responder)
	r.pushNotificationsClient.Send(ctx, pn.pn, responder)
	if err := <-responder; err != nil {
		var cErr error
		if errors.Is(err, push.ErrInvalidDeviceToken) {
			cErr = r.clearInvalidPushNotificationToken(ctx, pn.sn.UserID, pn.pn.Target)
		}

		return multierror.Append( //nolint:wrapcheck // Not needed.
			errors.Wrapf(cErr, "failed to clearInvalidPushNotificationToken for userID:%#v, push token:%#v", pn.sn.UserID, pn.pn.Target),
			errors.Wrapf(err, "failed to send push notification:%#v, desired to be sent:%#v", pn.pn, pn.sn),
			errors.Wrapf(r.deleteSentNotification(ctx, pn.sn), "failed to delete SENT_NOTIFICATIONS as a rollback for %#v", pn.sn),
		).ErrorOrNil()
	}

	return nil
}

func (r *repository) sendTelegramNotification(ctx context.Context, tn *telegramNotification) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	if err := r.insertSentNotification(ctx, tn.sn); err != nil {
		return errors.Wrapf(err, "failed to insert %#v", tn.sn)
	}
	responder := make(chan error, 1)
	defer close(responder)
	if err := r.telegramNotificationsClient.Send(ctx, tn.tn); err != nil {
		return multierror.Append( //nolint:wrapcheck // Not needed.
			errors.Wrapf(err, "failed to send telegram notification:%#v, desired to be sent:%#v", tn.sn, tn.sn),
			errors.Wrapf(r.deleteSentNotification(ctx, tn.sn), "failed to delete SENT_NOTIFICATIONS as a rollback for %#v", tn.sn),
		).ErrorOrNil()
	}

	return nil
}

func (r *repository) clearInvalidPushNotificationToken(ctx context.Context, userID string, token push.DeviceToken) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	sql := `UPDATE device_metadata
			SET push_notification_token = null
			WHERE user_id = $1
			  AND push_notification_token = $2`
	_, err := storage.Exec(ctx, r.db, sql, userID, token)

	return errors.Wrapf(err, "failed to update push_notification_token to empty for userID:%v and token %v", userID, token)
}

type (
	sentAnnouncementPK struct {
		Uniqueness               string              `json:"uniqueness,omitempty" example:"anything"`
		NotificationType         NotificationType    `json:"notificationType,omitempty" example:"adoption_changed"`
		NotificationChannel      NotificationChannel `json:"notificationChannel,omitempty" example:"email"`
		NotificationChannelValue string              `json:"notificationChannelValue,omitempty" example:"jdoe@example.com"`
	}
	sentAnnouncement struct {
		SentAt   *time.Time `json:"sentAt,omitempty" example:"2022-01-03T16:20:52.156534Z"`
		Language string     `json:"language,omitempty" example:"en"`
		sentAnnouncementPK
	}
	broadcastPushNotification[NOTIFICATION push.Notification[push.SubscriptionTopic] | push.DelayedNotification] struct {
		pn *NOTIFICATION     //nolint:structcheck // .
		sa *sentAnnouncement //nolint:structcheck // .
	}
)

func (r *repository) broadcastWithDelay(
	target push.SubscriptionTopic,
	bpn *broadcastPushNotification[push.Notification[push.SubscriptionTopic]],
) *broadcastPushNotification[push.DelayedNotification] {
	newBPN := new(broadcastPushNotification[push.DelayedNotification])
	newBPN.sa = bpn.sa
	newBPN.pn = new(push.DelayedNotification)
	newBPN.pn = &push.DelayedNotification{
		Notification: bpn.pn,
	}
	newBPN.pn.Target = target
	newBPN.sa.NotificationChannelValue = string(target)
	newBPN.pn.MaxDelaySec = r.cfg.MaxNotificationDelaySec
	newBPN.pn.MinDelaySec = r.cfg.MinNotificationDelaySec
	if delays, found := r.cfg.NotificationDelaysByTopic[bpn.pn.Target]; found {
		newBPN.pn.MaxDelaySec = delays.MaxNotificationDelaySec
		newBPN.pn.MinDelaySec = delays.MinNotificationDelaySec
	}

	return newBPN
}

func (r *repository) broadcastPushNotification(ctx context.Context, bpn *broadcastPushNotification[push.Notification[push.SubscriptionTopic]]) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}

	if err := r.insertSentAnnouncement(ctx, bpn.sa); err != nil {
		return errors.Wrapf(err, "failed to insert %#v", bpn.sa)
	}

	if err := r.pushNotificationsClient.Broadcast(ctx, bpn.pn); err != nil {
		return multierror.Append( //nolint:wrapcheck // .
			errors.Wrapf(err, "failed to broadcast push notification:%#v, desired to be sent:%#v", bpn.pn, bpn.sa),
			errors.Wrapf(r.deleteSentAnnouncement(ctx, bpn.sa), "failed to delete SENT_ANNOUNCEMENTS as a rollback for %#v", bpn.sa),
		).ErrorOrNil()
	}

	return nil
}

func (r *repository) broadcastPushNotificationDelayed(ctx context.Context, bpn *broadcastPushNotification[push.DelayedNotification]) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}

	if err := r.insertSentAnnouncement(ctx, bpn.sa); err != nil {
		return errors.Wrapf(err, "failed to insert %#v", bpn.sa)
	}

	if err := r.pushNotificationsClient.BroadcastDelayed(ctx, bpn.pn); err != nil {
		return multierror.Append( //nolint:wrapcheck // .
			errors.Wrapf(err, "failed to broadcast push notification:%#v, desired to be sent:%#v", bpn.pn, bpn.sa),
			errors.Wrapf(r.deleteSentAnnouncement(ctx, bpn.sa), "failed to delete SENT_ANNOUNCEMENTS as a rollback for %#v", bpn.sa),
		).ErrorOrNil()
	}

	return nil
}
