package main

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Role ...
type Role struct {
	ARN      string
	Provider string
}

// SAMLResponseAssertionAttribute ...
type SAMLResponseAssertionAttribute struct {
	AttributeValues []string `xml:"AttributeValue"`
}

// SAMLResponseAttributeStatement ...
type SAMLResponseAttributeStatement struct {
	Attributes []SAMLResponseAssertionAttribute `xml:"Attribute"`
}

// SAMLResponseAssertion ...
type SAMLResponseAssertion struct {
	AttributeStatement SAMLResponseAttributeStatement `xml:"AttributeStatement"`
}

// SAMLResponse ...
type SAMLResponse struct {
	XMLName   xml.Name              `xml:"Response"`
	Assertion SAMLResponseAssertion `xml:"Assertion"`
	Base64    string
}

// GetSAMLResponse ...
func GetSAMLResponse(username string, password string) (*SAMLResponse, error) {
	resp, err := GetADFSSAMLResponse(username, password)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	samlBase64Response, err := GetSAMLResponseFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	samlResponse := &SAMLResponse{
		Base64: samlBase64Response,
	}
	err = samlResponse.ParseFromBase64(samlBase64Response)
	if err != nil {
		return nil, err
	}

	return samlResponse, nil
}

func getADFSCookie(username string, password string) (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	data := url.Values{}
	data.Set("UserName", username)
	data.Add("Password", password)
	req, err := http.NewRequest(http.MethodPost, adfsURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	return resp.Header["Set-Cookie"][1], nil
}

// GetADFSSAMLResponse ...
func GetADFSSAMLResponse(username string, password string) (*http.Response, error) {
	data := url.Values{}
	data.Set("UserName", username)
	data.Add("Password", password)
	req, err := http.NewRequest(http.MethodPost, adfsURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	cookie, err := getADFSCookie(username, password)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Cookie", cookie)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// GetSAMLResponseFromReader ...
func GetSAMLResponseFromReader(r io.Reader) (string, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return "", err
	}

	matches := doc.Find("input[name=SAMLResponse]")
	if matches.Length() <= 0 {
		return "", errors.New("couldn't find SAMLResponse input")
	}

	var value string
	for i := range matches.Nodes {
		selection := matches.Eq(i)
		node := selection.Nodes[0]
		attributes := node.Attr
		for _, attr := range attributes {
			if attr.Key == "value" {
				value = attr.Val
			}
		}

		if value == "" {
			return "", nil
		}
	}

	return value, nil
}

// ParseFromBase64 ...
func (samlResponse *SAMLResponse) ParseFromBase64(encoded string) error {
	decodedSAMLResponse, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}

	err = xml.Unmarshal([]byte(decodedSAMLResponse), &samlResponse)
	if err != nil {
		return err
	}

	return nil
}

// GetRoles ...
func (samlResponse *SAMLResponse) GetRoles() []Role {
	rolesWithProviders := samlResponse.Assertion.AttributeStatement.Attributes[0].AttributeValues
	roles := []Role{}
	for _, roleWithProvider := range rolesWithProviders {
		split := strings.Split(roleWithProvider, ",")
		provider := split[0]
		role := split[1]
		if role == "" {
			continue
		}

		roles = append(roles, Role{
			ARN:      role,
			Provider: provider,
		})
	}

	return roles
}

// AssumeWithSAML ...
func (role *Role) AssumeWithSAML(saml string) (*sts.Credentials, error) {
	sess := session.Must(session.NewSession())
	credentials := credentials.NewStaticCredentials("", "", "")
	config := &aws.Config{Credentials: credentials, Region: aws.String("us-east-1")}
	svc := sts.New(sess, config)
	input := &sts.AssumeRoleWithSAMLInput{
		DurationSeconds: aws.Int64(3600),
		RoleArn:         aws.String(role.ARN),
		SAMLAssertion:   aws.String(saml),
		PrincipalArn:    aws.String(role.Provider),
	}
	resp, err := svc.AssumeRoleWithSAML(input)
	if err != nil {
		return nil, err
	}

	return resp.Credentials, nil
}
