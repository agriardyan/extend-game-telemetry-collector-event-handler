// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package bootstrap

import (
	"context"
	"log/slog"
	"os"

	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/factory"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/repository"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/service/iam"
	sdkAuth "github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/utils/auth"
)

// IAMServices holds IAM-related services and repositories
type IAMServices struct {
	OAuthService *iam.OAuth20Service
	TokenRepo    repository.TokenRepository
	ConfigRepo   repository.ConfigRepository
	RefreshRepo  repository.RefreshTokenRepository
}

// InitializeIAM sets up IAM authorization and performs client login
func InitializeIAM(ctx context.Context, logger *slog.Logger) (*IAMServices, error) {
	var tokenRepo repository.TokenRepository = sdkAuth.DefaultTokenRepositoryImpl()
	var configRepo repository.ConfigRepository = sdkAuth.DefaultConfigRepositoryImpl()
	var refreshRepo repository.RefreshTokenRepository = &sdkAuth.RefreshTokenImpl{RefreshRate: 0.8, AutoRefresh: true}

	oauthService := &iam.OAuth20Service{
		Client:                 factory.NewIamClient(configRepo),
		TokenRepository:        tokenRepo,
		RefreshTokenRepository: refreshRepo,
		ConfigRepository:       configRepo,
	}

	// Configure IAM authorization
	clientId := configRepo.GetClientId()
	clientSecret := configRepo.GetClientSecret()
	err := oauthService.LoginClient(&clientId, &clientSecret)
	if err != nil {
		logger.Error("error unable to login using clientId and clientSecret", "error", err)
		return nil, err
	}

	logger.Info("IAM services initialized")

	return &IAMServices{
		OAuthService: oauthService,
		TokenRepo:    tokenRepo,
		ConfigRepo:   configRepo,
		RefreshRepo:  refreshRepo,
	}, nil
}

// GetNamespace returns the namespace from environment or default
func GetNamespace() string {
	namespace := os.Getenv("AB_NAMESPACE")
	if namespace == "" {
		namespace = "accelbyte"
	}
	return namespace
}
