package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// email request payload for ZeptoMail API
type emailRequest struct {
	From     emailAddress   `json:"from"`
	To       []toRecipient  `json:"to"`
	Subject  string         `json:"subject"`
	HtmlBody string         `json:"htmlbody"`
}

type emailAddress struct {
	Address string `json:"address"`
}

type toRecipient struct {
	Email emailWithName `json:"email_address"`
}

type emailWithName struct {
	Address string `json:"address"`
	Name    string `json:"name"`
}

// SendEmail sends an HTML email using the ZeptoMail HTTP API
func SendEmail(to, subject, body string) error {
	apiURL := os.Getenv("ZEPTO_API_URL")     // e.g. https://api.zeptomail.com/v1.1/email
	apiKey := os.Getenv("ZEPTO_API_KEY")     // e.g. Zoho-enczapikey xxxxx
	from := os.Getenv("EMAIL_FROM")          // e.g. noreply@subsafe.co.ke
	toName := os.Getenv("EMAIL_TO_NAME")     // e.g. "User" or any name fallback

	if apiURL == "" || apiKey == "" || from == "" {
		log.Println("Missing ZEPTO_API_URL, ZEPTO_API_KEY, or EMAIL_FROM")
		return fmt.Errorf("missing required email config")
	}

	payload := emailRequest{
		From: emailAddress{Address: from},
		To: []toRecipient{
			{
				Email: emailWithName{
					Address: to,
					Name:    toName,
				},
			},
		},
		Subject:  subject,
		HtmlBody: body,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal email payload: %v", err)
		return err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to send email: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		log.Printf("ZeptoMail returned status %s", resp.Status)
		return fmt.Errorf("zeptomail API error: %s", resp.Status)
	}

	log.Printf("Email successfully sent to %s", to)
	return nil
}
