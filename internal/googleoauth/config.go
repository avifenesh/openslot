// Package googleoauth validates local OAuth configuration, not Google account consent.
package googleoauth

import "regexp"

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.apps\.googleusercontent\.com$`)

// Problem checks a web OAuth credential pair before it is saved or enabled.
// A correctly shaped pair still needs to be accepted by Google during sign-in.
func Problem(clientID string, secretSet bool) string {
	if clientID == "" {
		return "Add a Google OAuth web application Client ID and Client Secret."
	}
	if !clientIDPattern.MatchString(clientID) {
		return "Enter the full Google OAuth Client ID ending in .apps.googleusercontent.com, not a project ID or API key."
	}
	if !secretSet {
		return "Add the Client Secret for this Google OAuth client."
	}
	return ""
}
