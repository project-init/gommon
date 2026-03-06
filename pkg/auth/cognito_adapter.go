package auth

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/coreos/go-oidc"
	"github.com/golang-jwt/jwt/v5"
	gaws "github.com/project-init/gommon/pkg/aws"
	"github.com/project-init/gommon/pkg/cfg/configs"
	gerror "github.com/project-init/gommon/pkg/errors"
	"golang.org/x/oauth2"
)

type CognitoAdapter struct {
	cognitoClient *cognitoidentityprovider.Client
	appClientID   string
	userPoolID    string
	oauthHandler  *OauthHandler
}

var (
	stateCharacters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
)

// https://docs.aws.amazon.com/code-library/latest/ug/go_2_cognito-identity-provider_code_examples.html
// https://dev.to/wiliamvj/authentication-with-golang-and-aws-cognito-577e

// New creates a new Cognito adapter with the provided configuration.
// This function handles all the complex setup internally, including creating the Cognito client
// and OAuth handler based on whether you're using a local or production environment.
func New(cognitoConfig configs.Cognito, region string) (*CognitoAdapter, error) {
	var cognitoClient *cognitoidentityprovider.Client
	var oauthHandler *OauthHandler
	var err error

	if cognitoConfig.UseLocal {
		// Local development setup with custom endpoint
		cognitoClient = cognitoidentityprovider.New(cognitoidentityprovider.Options{
			BaseEndpoint: &cognitoConfig.Endpoint,
			Region:       region,
		})
	} else {
		// Production setup using AWS config
		cognitoClient = cognitoidentityprovider.NewFromConfig(gaws.GetConfig())
		oauthHandler, err = NewCognitoOauthHandler(cognitoConfig, []string{oidc.ScopeOpenID, "profile", "email"})
		if err != nil {
			return nil, err
		}
	}

	return &CognitoAdapter{
		cognitoClient: cognitoClient,
		appClientID:   cognitoConfig.ClientId,
		userPoolID:    cognitoConfig.UserPoolId,
		oauthHandler:  oauthHandler,
	}, nil
}

func (a *CognitoAdapter) SignUp(ctx context.Context, firstName string, lastName string, email string, password string) (bool, error) {
	output, err := a.cognitoClient.SignUp(ctx, &cognitoidentityprovider.SignUpInput{
		ClientId: aws.String(a.appClientID),
		Password: aws.String(password),
		Username: aws.String(email),
		UserAttributes: []types.AttributeType{
			{
				Name:  aws.String("email"),
				Value: aws.String(email),
			},
			{
				Name:  aws.String("given_name"),
				Value: aws.String(firstName),
			},
			{
				Name:  aws.String("family_name"),
				Value: aws.String(lastName),
			},
		},
	})
	if err != nil {
		var invalidPassword *types.InvalidPasswordException
		var userNameExists *types.UsernameExistsException
		if errors.As(err, &invalidPassword) {
			return false, fmt.Errorf("%w: invalid password - %s", gerror.ErrBadRequest, *invalidPassword.Message)
		} else if errors.As(err, &userNameExists) {
			return false, fmt.Errorf("%w: username exists - %s", gerror.ErrForbidden, *userNameExists.Message)
		}

		return false, fmt.Errorf("%w: SignUp failure - %s", err, email)
	}

	return output.UserConfirmed, err
}

func (a *CognitoAdapter) ResendConfirmationCode(ctx context.Context, username string) error {
	_, err := a.cognitoClient.ResendConfirmationCode(ctx, &cognitoidentityprovider.ResendConfirmationCodeInput{
		ClientId: aws.String(a.appClientID),
		Username: aws.String(username),
	})
	if err != nil {
		return fmt.Errorf("%w: ResendConfirmationCode failure - %s", err, username)
	}

	return nil
}

func (a *CognitoAdapter) ConfirmAccount(ctx context.Context, email string, code string) (bool, error) {
	confirmationInput := &cognitoidentityprovider.ConfirmSignUpInput{
		Username:         aws.String(email),
		ConfirmationCode: aws.String(code),
		ClientId:         aws.String(a.appClientID),
	}
	_, err := a.cognitoClient.ConfirmSignUp(ctx, confirmationInput)
	if err != nil {
		var mismatchException *types.CodeMismatchException
		var expiredCodeException *types.ExpiredCodeException
		if errors.As(err, &mismatchException) {
			return false, fmt.Errorf("%w: %s", gerror.ErrPreconditionFailed, *mismatchException.Message)
		} else if errors.As(err, &expiredCodeException) {
			return false, fmt.Errorf("%w: %s", gerror.ErrPreconditionFailed, *expiredCodeException.Message)
		}

		return false, err
	}

	return true, nil
}

func (a *CognitoAdapter) SignIn(ctx context.Context, email string, password string) (*types.AuthenticationResultType, error) {
	var authResult *types.AuthenticationResultType
	output, err := a.cognitoClient.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		AuthFlow:       types.AuthFlowTypeUserPasswordAuth,
		ClientId:       aws.String(a.appClientID),
		AuthParameters: map[string]string{"USERNAME": email, "PASSWORD": password},
	})
	if err != nil {
		var resetRequired *types.PasswordResetRequiredException
		var userNotFound *types.UserNotFoundException
		var notAuthorized *types.NotAuthorizedException
		var limitExceeded *types.LimitExceededException
		if errors.As(err, &resetRequired) {
			return nil, fmt.Errorf("%w: reset required - %s", gerror.ErrForbidden, *resetRequired.Message)
		} else if strings.Contains(err.Error(), "InvalidPasswordException: Invalid password") {
			// operation error Cognito Identity Provider: InitiateAuth, https response error StatusCode: 400, RequestID: , api error InvalidPasswordException: Invalid password
			// This is from cognito-local, and should really be a not authorized failure. Here just to cover our bases.
			return nil, fmt.Errorf("%w: invalid password - %s", gerror.ErrForbidden, err.Error())
		} else if errors.As(err, &userNotFound) {
			// User does not exist
			return nil, fmt.Errorf("%w: user not found - %s", gerror.ErrForbidden, *userNotFound.Message)
		} else if errors.As(err, &notAuthorized) {
			// User exists, but password given doesn't match
			return nil, fmt.Errorf("%w: not authorized - %s", gerror.ErrForbidden, *notAuthorized.Message)
		} else if errors.As(err, &limitExceeded) {
			// Too many failed login attempts
			return nil, fmt.Errorf("%w: %s", gerror.ErrTooManyRequests, *limitExceeded.Message)
		} else {
			return nil, fmt.Errorf("%w: couldn't sign in user %s. Reason - %s", gerror.ErrBadRequest, email, err.Error())
		}
	} else {
		authResult = output.AuthenticationResult
	}
	return authResult, err
}

func (a *CognitoAdapter) GetUserByToken(ctx context.Context, token string) (*CognitoUser, error) {
	input := &cognitoidentityprovider.GetUserInput{
		AccessToken: aws.String(token),
	}
	result, err := a.cognitoClient.GetUser(ctx, input)
	if err != nil {
		return nil, err
	}
	return userFromGetUserOutput(result), nil
}

func (a *CognitoAdapter) GetUsersByEmail(ctx context.Context, email string) ([]*CognitoUser, error) {
	input := &cognitoidentityprovider.ListUsersInput{
		UserPoolId: aws.String(a.userPoolID),
		Filter:     aws.String(fmt.Sprintf("email = \"%s\"", email)),
	}
	result, err := a.cognitoClient.ListUsers(ctx, input)
	if err != nil {
		return nil, err
	}
	return usersFromListUserOutput(result), nil
}

func (a *CognitoAdapter) UpdatePassword(ctx context.Context, email string, password string) error {
	input := &cognitoidentityprovider.AdminSetUserPasswordInput{
		UserPoolId: aws.String(a.userPoolID),
		Username:   aws.String(email),
		Password:   aws.String(password),
		Permanent:  true,
	}
	_, err := a.cognitoClient.AdminSetUserPassword(ctx, input)
	if err != nil {
		return err
	}
	return nil
}

func (a *CognitoAdapter) DeleteUser(ctx context.Context, accessToken string) error {
	_, err := a.cognitoClient.DeleteUser(ctx, &cognitoidentityprovider.DeleteUserInput{
		AccessToken: aws.String(accessToken),
	})
	if err != nil {
		return fmt.Errorf("%w: couldn't delete user", err)
	}
	return nil
}

func (a *CognitoAdapter) ForgotPassword(ctx context.Context, userName string) (*string, error) {
	output, err := a.cognitoClient.ForgotPassword(ctx, &cognitoidentityprovider.ForgotPasswordInput{
		ClientId: aws.String(a.appClientID),
		Username: aws.String(userName),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: couldn't start password reset for user", err)
	}
	return output.CodeDeliveryDetails.Destination, nil
}

func (a *CognitoAdapter) ResetPassword(ctx context.Context, code string, userName string, password string) error {
	_, err := a.cognitoClient.ConfirmForgotPassword(ctx, &cognitoidentityprovider.ConfirmForgotPasswordInput{
		ClientId:         aws.String(a.appClientID),
		ConfirmationCode: aws.String(code),
		Password:         aws.String(password),
		Username:         aws.String(userName),
	})
	if err != nil {
		var invalidPassword *types.InvalidPasswordException
		var limitExceeded *types.LimitExceededException
		var codeMismatch *types.CodeMismatchException
		if errors.As(err, &invalidPassword) {
			return fmt.Errorf("%w: %s", gerror.ErrBadRequest, *invalidPassword.Message)
		} else if errors.As(err, &limitExceeded) {
			return fmt.Errorf("%w: %s", gerror.ErrTooManyRequests, *limitExceeded.Message)
		} else if errors.As(err, &codeMismatch) {
			return fmt.Errorf("%w: %s", gerror.ErrPreconditionFailed, *codeMismatch.Message)
		}

		return fmt.Errorf("%w: couldn't confirm user %s", err, userName)
	}
	return nil
}

func (a *CognitoAdapter) UpdateAttributes(ctx context.Context, username string, attributes []types.AttributeType) error {
	input := cognitoidentityprovider.AdminUpdateUserAttributesInput{
		UserPoolId:     aws.String(a.userPoolID),
		Username:       aws.String(username),
		UserAttributes: attributes,
	}
	_, err := a.cognitoClient.AdminUpdateUserAttributes(ctx, &input)
	if err != nil {
		return fmt.Errorf("%w: couldn't update user attributes", err)
	}
	return nil
}

func (a *CognitoAdapter) OauthCodeUrl(state string) string {
	return a.oauthHandler.AuthCodeUrl(state, oauth2.AccessTypeOffline)
}

func GenerateState() string {
	b := make([]rune, 16)
	for i := range b {
		b[i] = stateCharacters[rand.IntN(len(stateCharacters))]
	}
	return string(b)
}

func (a *CognitoAdapter) ClaimsFromCode(ctx context.Context, code string) (jwt.MapClaims, error) {
	token, err := a.oauthHandler.ExchangeCode(ctx, code)
	if err != nil {
		return nil, err
	}

	rawIdToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("%w: couldn't extract id_token", gerror.ErrBadRequest)
	}
	verifier := a.oauthHandler.Verifier()
	idToken, err := verifier.Verify(ctx, rawIdToken)
	if err != nil {
		return nil, fmt.Errorf("%w: couldn't verify id_token", gerror.ErrBadRequest)
	}

	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: couldn't extract claims", gerror.ErrBadRequest)
	}
	return claims, nil
}
