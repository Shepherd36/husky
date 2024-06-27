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

func (r *repository) addScheduledSocialsNotifications(ctx context.Context, us *users.User) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	now := time.Now()
	scheduled := make([]*scheduledNotification, 0, len(r.cfg.Socials))
	var dayDuration stdlibtime.Duration
	if r.cfg.Development {
		dayDuration = 1 * stdlibtime.Minute
	} else {
		dayDuration = 24 * stdlibtime.Hour
	}
	for ix := range r.cfg.Socials {
		scheduled = append(scheduled, &scheduledNotification{
			ScheduledAt:              now,
			ScheduledFor:             time.New(us.CreatedAt.Add(stdlibtime.Duration((ix + 1)) * dayDuration)),
			Language:                 us.Language,
			UserID:                   us.ID,
			NotificationType:         r.cfg.Socials[ix].NotificationType,
			Uniqueness:               fmt.Sprintf("%v_%v_%vd", us.ID, r.cfg.Socials[ix].NotificationType, ix+1),
			NotificationChannel:      string(PushNotificationChannel),
			NotificationChannelValue: us.ID,
			Data: &users.JSON{
				"SocialUrl": r.cfg.Socials[ix].Link,
			},
		})
	}

	return errors.Wrapf(r.insertScheduledNotifications(ctx, scheduled), "can't execute insertScheduledNotifications:%#v", scheduled)
}
