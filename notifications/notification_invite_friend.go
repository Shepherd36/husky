// SPDX-License-Identifier: ice License 1.0

package notifications

import (
	"context"
	"fmt"
	stdlibtime "time"

	"github.com/pkg/errors"

	"github.com/ice-blockchain/eskimo/users"
	"github.com/ice-blockchain/wintr/time"
)

//nolint:funlen // .
func (r *repository) addScheduledInviteFriendNotifications(ctx context.Context, us *users.User) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	now := time.Now()
	var dayDuration, firstNotificationDuration stdlibtime.Duration
	if r.cfg.Development {
		dayDuration = 1 * stdlibtime.Minute
		firstNotificationDuration = 1 * stdlibtime.Minute
	} else {
		dayDuration = 24 * stdlibtime.Hour
		firstNotificationDuration = 1 * stdlibtime.Hour
	}
	availableChannels := []NotificationChannel{PushNotificationChannel}
	scheduled := make([]*scheduledNotification, 0, len(availableChannels))
	for _, channel := range availableChannels {
		scheduled = append(scheduled, &scheduledNotification{
			ScheduledAt:              now,
			ScheduledFor:             time.New(us.CreatedAt.Add(firstNotificationDuration)),
			Language:                 us.Language,
			UserID:                   us.ID,
			NotificationType:         string(InviteFriendNotificationType),
			Uniqueness:               fmt.Sprintf("%v_%v_1h", us.ID, InviteFriendNotificationType),
			NotificationChannel:      string(channel),
			NotificationChannelValue: us.ID,
			Data: &users.JSON{
				"TenantName": r.cfg.TenantName,
				"Username":   us.Username,
			},
		}, &scheduledNotification{
			ScheduledAt:              now,
			ScheduledFor:             time.New(us.CreatedAt.Add(7 * dayDuration)), //nolint:gomnd,mnd // .
			Language:                 us.Language,
			UserID:                   us.ID,
			NotificationType:         string(InviteFriendNotificationType),
			Uniqueness:               fmt.Sprintf("%v_%v_7d", us.ID, InviteFriendNotificationType),
			NotificationChannel:      string(channel),
			NotificationChannelValue: us.ID,
			Data: &users.JSON{
				"TenantName": r.cfg.TenantName,
				"Username":   us.Username,
			},
		})
	}

	return errors.Wrapf(insertScheduledNotifications(ctx, r.db, scheduled), "can't execute insertScheduledNotifications:%#v", scheduled)
}
