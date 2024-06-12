// SPDX-License-Identifier: ice License 1.0

package main

import (
	"context"
	"strings"
	stdlibtime "time"

	"github.com/pkg/errors"

	"github.com/ice-blockchain/husky/news"
	"github.com/ice-blockchain/husky/notifications"
	"github.com/ice-blockchain/wintr/server"
	"github.com/ice-blockchain/wintr/time"
)

// Public API.

type (
	GetNotificationChannelTogglesArg struct {
		NotificationChannel notifications.NotificationChannel `uri:"notificationChannel" example:"push" enums:"push,email" required:"true"`
	}
	GetNewsArg struct {
		// Default is `regular`.
		Type         news.Type `form:"type" example:"regular" enums:"regular,featured"`
		CreatedAfter string    `form:"createdAfter" example:"2022-01-03T16:20:52.156534Z"`
		Language     string    `uri:"language" example:"en" required:"true"`
		Limit        uint64    `form:"limit" maximum:"1000" example:"10"` // 10 by default.
		Offset       uint64    `form:"offset" example:"5"`
	}
	GetUnreadNewsCountArg struct {
		CreatedAfter string `form:"createdAfter" example:"2022-01-03T16:20:52.156534Z"`
		Language     string `uri:"language" example:"en" required:"true"`
	}
)

// Private API.

func (s *service) registerReadRoutes(router *server.Router) {
	s.setupNewsReadRoutes(router)
	s.setupNotificationsReadRoutes(router)
}

func (s *service) setupNewsReadRoutes(router *server.Router) {
	router.
		Group("v1r").
		GET("news/:language", server.RootHandler(s.GetNews)).
		GET("unread-news-count/:language", server.RootHandler(s.GetUnreadNewsCount))
}

// GetNews godoc
//
//	@Schemes
//	@Description	Returns a list of news.
//	@Tags			News
//	@Accept			json
//	@Produce		json
//	@Param			Authorization	header		string	true	"Insert your access token"							default(Bearer <Add access token here>)
//	@Param			type			query		string	false	"type of news to look for. Default is `regular`."	enums(regular,featured)
//	@Param			language		path		string	true	"the language of the news article"
//	@Param			limit			query		uint64	false	"Limit of elements to return. Defaults to 10"
//	@Param			offset			query		uint64	false	"Elements to skip before starting to look for"
//	@Param			createdAfter	query		string	false	"Example `2022-01-03T16:20:52.156534Z`. If unspecified, the creation date of the news articles will be ignored."
//	@Success		200				{array}		news.PersonalNews
//	@Failure		400				{object}	server.ErrorResponse	"if validations fail"
//	@Failure		401				{object}	server.ErrorResponse	"if not authorized"
//	@Failure		422				{object}	server.ErrorResponse	"if syntax fails"
//	@Failure		500				{object}	server.ErrorResponse
//	@Failure		504				{object}	server.ErrorResponse	"if request times out"
//	@Router			/v1r/news/{language} [GET].
func (s *service) GetNews( //nolint:gocritic,funlen // False negative.
	ctx context.Context,
	req *server.Request[GetNewsArg, []*news.PersonalNews],
) (*server.Response[[]*news.PersonalNews], *server.Response[server.ErrorResponse]) {
	var createdAfter *time.Time
	if req.Data.CreatedAfter == "" {
		createdAfter = time.New(stdlibtime.Unix(0, 0).UTC())
	} else {
		createdAfter = new(time.Time)
		if err := createdAfter.UnmarshalJSON(ctx, []byte(`"`+req.Data.CreatedAfter+`"`)); err != nil {
			return nil, server.UnprocessableEntity(errors.Errorf("invalid createdAfter `%v`", req.Data.CreatedAfter), invalidPropertiesErrorCode)
		}
	}
	if req.Data.Type == "" {
		req.Data.Type = news.RegularNewsType
	}
	if req.Data.Type == news.FeaturedNewsType {
		req.Data.Limit, req.Data.Offset = 1, 0
	}
	if req.Data.Limit == 0 {
		req.Data.Limit = 10
	}
	if req.Data.Limit > 1000 { //nolint:gomnd,mnd //.
		req.Data.Limit = 1000
	}
	req.Data.Language = strings.ToLower(req.Data.Language)
	if _, validLanguage := languages[req.Data.Language]; !validLanguage {
		return nil, server.BadRequest(errors.Errorf("invalid language `%v`", req.Data.Language), invalidPropertiesErrorCode)
	}
	if req.Data.Type != news.RegularNewsType && req.Data.Type != news.FeaturedNewsType {
		return nil, server.BadRequest(errors.Errorf("invalid type %v", req.Data.Type), invalidPropertiesErrorCode)
	}
	resp, err := s.newsProcessor.GetNews(ctx, req.Data.Type, req.Data.Language, req.Data.Limit, req.Data.Offset, createdAfter)
	if err != nil {
		return nil, server.Unexpected(errors.Wrapf(err, "failed to get news by %#v", req.Data))
	}

	return server.OK(&resp), nil
}

// GetUnreadNewsCount godoc
//
//	@Schemes
//	@Description	Returns the number of unread news the authorized user has.
//	@Tags			News
//	@Accept			json
//	@Produce		json
//	@Param			Authorization	header		string	true	"Insert your access token"	default(Bearer <Add access token here>)
//	@Param			language		path		string	true	"The language of the news to be counted"
//	@Param			createdAfter	query		string	false	"Example `2022-01-03T16:20:52.156534Z`. If unspecified, the creation date of the news articles will be ignored."
//	@Success		200				{object}	news.UnreadNewsCount
//	@Failure		401				{object}	server.ErrorResponse	"if not authorized"
//	@Failure		422				{object}	server.ErrorResponse	"if syntax fails"
//	@Failure		500				{object}	server.ErrorResponse
//	@Failure		504				{object}	server.ErrorResponse	"if request times out"
//	@Router			/v1r/unread-news-count/{language} [GET].
func (s *service) GetUnreadNewsCount( //nolint:gocritic // False negative.
	ctx context.Context,
	req *server.Request[GetUnreadNewsCountArg, news.UnreadNewsCount],
) (*server.Response[news.UnreadNewsCount], *server.Response[server.ErrorResponse]) {
	var createdAfter *time.Time
	if req.Data.CreatedAfter == "" {
		createdAfter = time.New(stdlibtime.Unix(0, 0).UTC())
	} else {
		createdAfter = new(time.Time)
		if err := createdAfter.UnmarshalJSON(ctx, []byte(`"`+req.Data.CreatedAfter+`"`)); err != nil {
			return nil, server.UnprocessableEntity(errors.Errorf("invalid createdAfter `%v`", req.Data.CreatedAfter), invalidPropertiesErrorCode)
		}
	}
	req.Data.Language = strings.ToLower(req.Data.Language)
	if _, validLanguage := languages[req.Data.Language]; !validLanguage {
		return nil, server.BadRequest(errors.Errorf("invalid language `%v`", req.Data.Language), invalidPropertiesErrorCode)
	}
	resp, err := s.newsProcessor.GetUnreadNewsCount(ctx, req.Data.Language, createdAfter)
	if err != nil {
		return nil, server.Unexpected(errors.Wrapf(err, "failed to get unread news count for userID:%v", req.AuthenticatedUser.UserID))
	}

	return server.OK(resp), nil
}

func (s *service) setupNotificationsReadRoutes(router *server.Router) {
	router.
		Group("v1r").
		GET("notification-channels/:notificationChannel/toggles", server.RootHandler(s.GetNotificationChannelToggles))
}

// GetNotificationChannelToggles godoc
//
//	@Schemes
//	@Description	Returns the user's list of notification channel toggles for the provided notificationChannel.
//	@Tags			Notifications
//	@Accept			json
//	@Produce		json
//	@Param			Authorization		header		string	true	"Insert your access token"	default(Bearer <Add access token here>)
//	@Param			notificationChannel	path		string	true	"email/push"				enums(push,email)
//	@Success		200					{array}		notifications.NotificationChannelToggle
//	@Failure		400					{object}	server.ErrorResponse	"if validations fail"
//	@Failure		401					{object}	server.ErrorResponse	"if not authorized"
//	@Failure		422					{object}	server.ErrorResponse	"if syntax fails"
//	@Failure		500					{object}	server.ErrorResponse
//	@Failure		504					{object}	server.ErrorResponse	"if request times out"
//	@Router			/v1r/notification-channels/{notificationChannel}/toggles [GET].
func (s *service) GetNotificationChannelToggles( //nolint:gocritic // False negative.
	ctx context.Context,
	req *server.Request[GetNotificationChannelTogglesArg, []*notifications.NotificationChannelToggle],
) (*server.Response[[]*notifications.NotificationChannelToggle], *server.Response[server.ErrorResponse]) {
	if req.Data.NotificationChannel != notifications.PushNotificationChannel && req.Data.NotificationChannel != notifications.EmailNotificationChannel {
		return nil, server.UnprocessableEntity(errors.Errorf("invalid notificationChannel `%v`", req.Data.NotificationChannel), invalidPropertiesErrorCode)
	}
	resp, err := s.notificationsProcessor.GetNotificationChannelToggles(ctx, req.Data.NotificationChannel, req.AuthenticatedUser.UserID)
	if err != nil {
		return nil, server.Unexpected(errors.Wrapf(err, "failed to GetNotificationChannelToggles for %#v, userID:%v", req.Data, req.AuthenticatedUser.UserID))
	}

	return server.OK(&resp), nil
}
