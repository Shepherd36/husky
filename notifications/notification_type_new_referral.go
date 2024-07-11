// SPDX-License-Identifier: ice License 1.0

package notifications

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	"github.com/ice-blockchain/eskimo/users"
	"github.com/ice-blockchain/freezer/model"
	"github.com/ice-blockchain/freezer/tokenomics"
	storagev3 "github.com/ice-blockchain/wintr/connectors/storage/v3"
	"github.com/ice-blockchain/wintr/log"
	"github.com/ice-blockchain/wintr/notifications/inapp"
	"github.com/ice-blockchain/wintr/notifications/push"
	"github.com/ice-blockchain/wintr/time"
)

func (r *repository) sendNewReferralNotification(ctx context.Context, us *users.UserSnapshot) error { //nolint:funlen,gocyclo,revive,cyclop // .
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "unexpected deadline")
	}
	usernameEmpty := us.Username == "" || us.Username == us.ID
	referredByNotChanged := us.Before != nil && us.Before.ID != "" && us.User != nil && us.User.ID != "" && us.User.ReferredBy == us.Before.ReferredBy
	if us.User == nil || us.User.ReferredBy == "" || us.User.ReferredBy == us.User.ID ||
		(referredByNotChanged && !usernameEmpty && us.Before.Username != "" && us.Before.Username != us.Before.ID) ||
		usernameEmpty {
		return nil
	}

	const (
		actionName = "referral_joined_team"
	)
	now := time.Now()
	deeplink := fmt.Sprintf("%v://profile?userId=%v", r.cfg.DeeplinkScheme, us.User.ID)
	in := &inAppNotification{
		in: &inapp.Parcel{
			Time:        now,
			ReferenceID: fmt.Sprintf("%v:userId:%v", actionName, us.User.ID),
			Data: map[string]any{
				"username": us.User.Username,
				"deeplink": deeplink,
				"imageUrl": us.User.ProfilePictureURL,
			},
			Action: actionName,
			Actor: inapp.ID{
				Type:  "userId",
				Value: us.User.ID,
			},
			Subject: inapp.ID{
				Type:  "userId",
				Value: us.User.ReferredBy,
			},
		},
		sn: &sentNotification{
			SentAt: now,
			sentNotificationPK: sentNotificationPK{
				UserID:              us.User.ReferredBy,
				Uniqueness:          us.User.ID,
				NotificationType:    NewReferralNotificationType,
				NotificationChannel: InAppNotificationChannel,
			},
		},
	}
	tokens, err := r.getPushNotificationTokens(ctx, MicroCommunityNotificationDomain, us.User.ReferredBy)
	if err != nil || tokens == nil || tokens.PushNotificationTokens == nil || len(*tokens.PushNotificationTokens) == 0 {
		return multierror.Append( //nolint:wrapcheck // .
			err,
			errors.Wrapf(r.sendInAppNotification(ctx, in), "failed to sendInAppNotification for %v, notif:%#v", NewReferralNotificationType, in),
		).ErrorOrNil()
	}
	tmpl, found := allPushNotificationTemplates[NewReferralNotificationType][tokens.Language]
	if !found {
		log.Warn(fmt.Sprintf("language `%v` was not found in the `%v` push config", tokens.Language, NewReferralNotificationType))

		return errors.Wrapf(r.sendInAppNotification(ctx, in), "failed to sendInAppNotification for %v, notif:%#v", NewReferralNotificationType, in)
	}
	pn := make([]*pushNotification, 0, len(*tokens.PushNotificationTokens))
	data := struct {
		Username, Coin string
		Amount         uint64
	}{
		Username: fmt.Sprintf("@%v", us.User.Username),
		Coin:     r.cfg.TokenName,
		Amount:   r.getNewReferralCoinAmount(ctx, us.User.ReferredBy),
	}
	for _, token := range *tokens.PushNotificationTokens {
		var body string
		if data.Amount > 0 {
			body = tmpl.getBody(data)
		} else {
			body = tmpl.getAltBody(nil)
		}
		pn = append(pn, &pushNotification{
			pn: &push.Notification[push.DeviceToken]{
				Data:     map[string]string{"deeplink": deeplink},
				Target:   token,
				Title:    tmpl.getTitle(data),
				Body:     body,
				ImageURL: us.User.ProfilePictureURL,
			},
			sn: &sentNotification{
				SentAt:   now,
				Language: tokens.Language,
				sentNotificationPK: sentNotificationPK{
					UserID:                   us.User.ReferredBy,
					Uniqueness:               us.User.ID,
					NotificationType:         NewReferralNotificationType,
					NotificationChannel:      PushNotificationChannel,
					NotificationChannelValue: string(token),
				},
			},
		})
	}

	return errors.Wrap(executeConcurrently(func() error {
		return errors.Wrapf(runConcurrently(ctx, r.sendPushNotification, pn), "failed to sendPushNotifications atleast to some devices for %v, args:%#v", NewReferralNotificationType, pn) //nolint:lll // .
	}, func() error {
		return errors.Wrapf(r.sendInAppNotification(ctx, in), "failed to sendInAppNotification for %v, notif:%#v", NewReferralNotificationType, in)
	}), "failed to executeConcurrently")
}

func (r *repository) getNewReferralCoinAmount(ctx context.Context, referredBy string) uint64 {
	freezerInternalID, err := tokenomics.GetInternalID(ctx, r.freezerDB, referredBy)
	if err != nil {
		log.Error(errors.Wrapf(err, "failed to tokenomics.GetInternalID for referredBy: %v", referredBy))

		return uint64(tokenomics.WelcomeBonusV2Amount)
	}
	state, err := storagev3.Get[struct {
		model.UserIDField
		model.PreStakingBonusField
		model.BalanceT1WelcomeBonusPendingField
	}](ctx, r.freezerDB, model.SerializedUsersKey(freezerInternalID))
	if err != nil || len(state) == 0 {
		if err == nil {
			err = errors.Wrapf(ErrRelationNotFound, "missing state for freezerInternalID:%v referredBy:%v", freezerInternalID, referredBy)
		}
		log.Error(errors.Wrapf(err, "failed to get PreStakingBonus for freezerInternalID:%v referredBy:%v", freezerInternalID, referredBy))

		return uint64(tokenomics.WelcomeBonusV2Amount)
	}
	if state[0].BalanceT1WelcomeBonusPending >= 25*tokenomics.WelcomeBonusV2Amount {
		return 0
	}
	if state[0].PreStakingBonus == 0 {
		return uint64(tokenomics.WelcomeBonusV2Amount)
	}

	return uint64((state[0].PreStakingBonus + 100.0) * tokenomics.WelcomeBonusV2Amount / 100.0) //nolint:gomnd,mnd // Nope.
}
