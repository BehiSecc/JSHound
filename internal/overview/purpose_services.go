package overview

import (
	"sort"
	"strings"
)

// thirdParty identifies a service by the hosts its SDK talks to or loads from,
// or by a global it defines. Plain links to a company's website are not
// markers, so a page that merely mentions a service does not count.
type thirdParty struct {
	name, role string
	hosts      []string
	globals    []string
}

const maxThirdParties = 6

var knownServices = []thirdParty{
	{"Sentry", "error reporting", []string{"sentry.io", "sentry-cdn.com"}, []string{"__SENTRY__"}},
	{"Slardar", "monitoring", []string{"slardar"}, []string{"slardar", "Slardar"}},
	{"Datadog", "monitoring", []string{"datadoghq.com", "datadoghq.eu"}, []string{"DD_RUM", "DD_LOGS"}},
	{"New Relic", "monitoring", []string{"nr-data.net", "js-agent.newrelic.com"}, []string{"NREUM"}},
	{"Bugsnag", "error reporting", []string{"bugsnag.com"}, []string{"Bugsnag"}},
	{"LogRocket", "session replay", []string{"logrocket.io", "lr-ingest.io", "lr-in.com"}, nil},
	{"FullStory", "session replay", []string{"fullstory.com"}, nil},
	{"Hotjar", "session replay", []string{"hotjar.com"}, nil},
	{"Microsoft Clarity", "session replay", []string{"clarity.ms"}, nil},
	{"Google Analytics", "analytics", []string{"google-analytics.com", "googletagmanager.com"}, []string{"gtag"}},
	{"Segment", "analytics", []string{"cdn.segment.com", "api.segment.io"}, nil},
	{"Mixpanel", "analytics", []string{"mixpanel.com"}, []string{"mixpanel"}},
	{"Amplitude", "analytics", []string{"amplitude.com"}, nil},
	{"Heap", "analytics", []string{"heapanalytics.com"}, nil},
	{"PostHog", "analytics", []string{"posthog.com"}, []string{"posthog"}},
	{"Facebook Pixel", "ads", []string{"connect.facebook.net"}, []string{"fbq"}},
	{"TikTok Pixel", "ads", []string{"analytics.tiktok.com"}, []string{"ttq"}},
	{"Google Ads", "ads", []string{"googleadservices.com", "googlesyndication.com", "doubleclick.net"}, nil},
	{"LinkedIn Insight", "ads", []string{"snap.licdn.com"}, nil},
	{"X Pixel", "ads", []string{"static.ads-twitter.com"}, nil},
	{"reCAPTCHA", "bot checks", []string{"google.com/recaptcha", "recaptcha.net", "gstatic.com/recaptcha"}, []string{"grecaptcha"}},
	{"hCaptcha", "bot checks", []string{"hcaptcha.com"}, []string{"hcaptcha"}},
	{"Cloudflare Turnstile", "bot checks", []string{"challenges.cloudflare.com"}, []string{"turnstile"}},
	{"Stripe", "payments", []string{"js.stripe.com", "api.stripe.com"}, nil},
	{"PayPal", "payments", []string{"paypal.com/sdk", "paypalobjects.com"}, nil},
	{"Braintree", "payments", []string{"braintreegateway.com"}, nil},
	{"Firebase", "backend services", []string{"firebaseio.com", "firebaseapp.com", "firebase.googleapis.com"}, nil},
	{"Supabase", "backend services", []string{"supabase.co"}, nil},
	{"Auth0", "sign-in", []string{"auth0.com"}, nil},
	{"Okta", "sign-in", []string{"okta.com", "oktacdn.com"}, nil},
	{"Google Sign-In", "sign-in", []string{"accounts.google.com/gsi", "apis.google.com/js/platform"}, nil},
	{"Sign in with Apple", "sign-in", []string{"appleid.cdn-apple.com", "appleid.apple.com/auth"}, nil},
	{"Intercom", "support chat", []string{"intercom.io", "intercomcdn.com"}, nil},
	{"Zendesk", "support chat", []string{"zdassets.com"}, nil},
	{"Algolia", "search", []string{"algolia.net", "algolianet.com"}, nil},
	{"Google Maps", "maps", []string{"maps.googleapis.com"}, nil},
	{"Mapbox", "maps", []string{"api.mapbox.com"}, nil},
	{"LaunchDarkly", "feature flags", []string{"launchdarkly.com"}, nil},
	{"Optimizely", "experiments", []string{"optimizely.com"}, nil},
	{"Statsig", "feature flags", []string{"statsigapi.net", "featuregates.org"}, nil},
	{"Pusher", "real-time messaging", []string{"pusher.com", "pusherapp.com"}, nil},
	{"Ably", "real-time messaging", []string{"ably.io", "ably-realtime.com"}, nil},
	{"OneSignal", "push notifications", []string{"onesignal.com"}, nil},
	{"Cloudinary", "media hosting", []string{"res.cloudinary.com"}, nil},
	{"YouTube player", "video", []string{"youtube.com/iframe_api"}, nil},
}

var (
	identifierServices = map[string][]string{}
	serviceRole        = map[string]string{}
)

func init() {
	for _, service := range knownServices {
		serviceRole[service.name] = service.role
		for _, global := range service.globals {
			identifierServices[global] = append(identifierServices[global], service.name)
		}
	}
}

// stringServiceMatches returns services whose SDK host appears in a
// lowercase string value.
func stringServiceMatches(lower string) []string {
	if !strings.Contains(lower, ".") && !strings.Contains(lower, "slardar") {
		return nil
	}
	var names []string
	for _, service := range knownServices {
		for _, host := range service.hosts {
			if strings.Contains(lower, host) {
				names = append(names, service.name)
				break
			}
		}
	}
	return names
}

// topServices ranks the most referenced services, most used first.
func topServices(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	return names[:min(len(names), maxThirdParties)]
}

// thirdParties lists services with their roles, such as
// "Slardar (monitoring), Google Analytics (analytics)".
func thirdParties(names []string) string {
	var parts []string
	for _, name := range names {
		parts = append(parts, name+" ("+serviceRole[name]+")")
	}
	return strings.Join(parts, ", ")
}
