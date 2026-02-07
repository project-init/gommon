package auth

import (
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

type CognitoUser struct {
	Username      string
	Email         string
	EmailVerified bool
	GivenName     string
	FamilyName    string
	PhoneNumber   string
}

func userFromGetUserOutput(output *cognitoidentityprovider.GetUserOutput) *CognitoUser {
	cognitoUser := &CognitoUser{
		Username: *output.Username,
	}

	for _, attribute := range output.UserAttributes {
		setAttribute(cognitoUser, attribute)
	}

	return cognitoUser
}

func usersFromListUserOutput(output *cognitoidentityprovider.ListUsersOutput) []*CognitoUser {
	users := make([]*CognitoUser, len(output.Users))
	for index, user := range output.Users {
		cognitoUser := &CognitoUser{
			Username: *user.Username,
		}

		for _, attribute := range user.Attributes {
			setAttribute(cognitoUser, attribute)
		}
		users[index] = cognitoUser
	}

	return users
}

func setAttribute(cognitoUser *CognitoUser, attribute types.AttributeType) {
	switch *attribute.Name {
	case "email":
		cognitoUser.Email = *attribute.Value
	case "email_verified":
		emailVerified, err := strconv.ParseBool(*attribute.Value)
		if err == nil {
			cognitoUser.EmailVerified = emailVerified
		}
	case "family_name":
		cognitoUser.FamilyName = *attribute.Value
	case "given_name":
		cognitoUser.GivenName = *attribute.Value
	case "phone_number":
		cognitoUser.PhoneNumber = *attribute.Value
	}
}
