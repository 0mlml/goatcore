package welcome

// The guide's content model: WHO the reader is, WHAT their process is, and
// WHERE the depth lives.
//
// The first version of this page was one document with sections hidden by tier.
// That is not the same as documenting a role's process, and it showed: iam-admin
// and platform-admin rendered byte-identical pages, and the site network admin —
// a real role with its own rules — had no page at all.
//
// So the model is explicit. A ROLE owns an ordered path (what this person does,
// in their order, in their words) plus the set of deep dives that apply to them.
// The index stays light and continuous; anything that would bloat it becomes a
// TOPIC the reader clicks into. Nothing is hidden from anyone — every topic is
// reachable by URL — but a role's index shows only what its own work needs.

import (
	"fmt"
	"strings"
)

// Role is one documented process, not a permission check. The guide is
// unauthenticated and mesh-only; these views exist so a reader gets THEIR
// instructions, not so anything is withheld.
type Role struct {
	Key     string // url value, e.g. "iam-admin"
	Title   string // "Identity admin"
	OneLine string // what this role is, in one sentence
	// Path is the role's own sequence. Each step is a heading plus a short
	// paragraph — deliberately terse, because depth belongs in a Topic.
	Path []PathStep
	// Topics are the deep dives this role's index links, in order.
	Topics []string
	// Repos are catalog slugs worth this role's time, most relevant first.
	Repos []string
}

// PathStep is one move in a role's process.
type PathStep struct {
	// ID is a stable handle for steps the renderer has to SWAP per goatnet.
	// Only steps that vary carry one. Everything else leaves it empty.
	ID    string
	Title string
	Body  string
	// Topic, when set, is the deep dive this step opens into.
	Topic string
	// Mins is roughly how long this step takes to DO — not to read about. A
	// curriculum that does not say what it costs gets started at the wrong
	// moment and abandoned halfway; the per-step figure lets somebody pick up
	// one step in a gap rather than needing to clear an afternoon.
	Mins int
	// Clock, when set, marks this step as TIME-SENSITIVE. It renders as its own
	// marked element, never as another sentence in the body — an expiry buried
	// in a paragraph is an expiry someone reads after it has passed, and the
	// cost of missing one here is a credential that has to be re-issued by an
	// admin.
	Clock *Deadline
}

// Deadline is something that stops working if the reader waits.
type Deadline struct {
	// Window is the human span, e.g. "about 24 hours", "15 minutes". It is
	// rendered as "<What> lasts <Window>.", so it must read as a DURATION and
	// nothing else. Writing a clause here produced "the portal key + bundle on
	// the fabric-credential page lasts the mint page works only ONCE." — use
	// Limit for a constraint that is not a span of time.
	Window string
	// Limit is the alternative to Window for something that does not expire but
	// is still bounded — a single-use page, a one-shot collection. Rendered as
	// "<What> <Limit>.", so it reads on from What as a predicate. Exactly one
	// of Window or Limit should be set; Window wins if both are.
	Limit string
	// What lapses.
	What string
	// Recover says what to do if it already lapsed — every deadline on this
	// page is recoverable, and saying so stops a missed window from reading as
	// a locked door.
	Recover string
}

// HOW A PERSON GETS THEIR FABRIC CREDENTIAL IS PER-GOATNET CONFIG, NOT COPY.
//
// A goatnet either defers the credential — the person mints the portal key and
// bundle themselves, once, on the console's fabric-credential page — or it
// mints at provisioning, in which case the key is printed in the welcome email
// and the bundle arrives as a one-time link in it. That is one flag
// (`onboard_runner_defer_credential`), and both the welcome EMAIL and this guide
// branch on it: a single unconditional description would tell readers of the
// other kind of goatnet the opposite of what their own email says. So the
// variants live here as data and the renderer picks.
var credentialStepMintOnCollect = PathStep{
	ID: "credential", Title: "Mint your fabric credential — once, yourself",
	Body: "Your portal API key, certificate, private key and CLI profile do not travel by email: you mint them once on the console's fabric-credential page (sign in with your password first, on the mesh). The page shows the portal key and the bundle ONCE — copy the key into the Portal's Sign in box then, and download the bundle. If you use the goat CLI, run `goat ca add <the -ca.pem from the bundle>` and then `goat profile init <the .cbor>` — goat trusts no CA until you add one, so the order matters. Never copy the key out of the .cbor by hand; it is a binary file and a hand-copy corrupts the key.",
	Mins: 5, Topic: "fabric",
	// No expiry here — the page stays available until you use it, and the
	// second attempt is refused (already_minted). So the bound is single-use,
	// not a span, and it needs Limit rather than Window.
	Clock: &Deadline{Limit: "does not expire, but works only ONCE",
		What:    "the portal key + bundle on the fabric-credential page",
		Recover: "ask an admin to re-issue (rotate) your credential — this invalidates the previous key and bundle, so only do it if they are truly lost"},
}

// credentialStepEmailed is the other live shape: provisioning already minted
// for you, so the work is collecting from the mail you were sent rather than
// visiting a page. It says where each half is, because the two halves travel
// differently — the key as text in the mail, the bundle as a one-time link.
var credentialStepEmailed = PathStep{
	ID: "credential", Title: "Collect your fabric credential from your welcome email",
	Body: "Your credential was minted for you when your account was created, so there is no page to visit: your portal API key is printed in the welcome email itself, and the certificate, private key and CLI profile come from the one-time bundle link in that same email. The key material never travels as an attachment — the link is mesh-only and single-use, which is why it expires. Paste the key into the Portal's Sign in box, and if you use the goat CLI run `goat ca add <the -ca.pem from the bundle>` and then `goat profile init <the .cbor>` — goat trusts no CA until you add one, so the order matters. Never copy the key out of the .cbor by hand; it is a binary file and a hand-copy corrupts the key.",
	Mins: 5, Topic: "fabric",
	Clock: &Deadline{Window: "about 24 hours, and it works only ONCE",
		What:    "the bundle download link in your welcome email",
		Recover: "ask an admin to re-send your welcome package — this does not invalidate anything you already hold"},
}

// CredentialStepFor returns the step that matches how THIS goatnet issues
// credentials. Exported so the render path and its tests share one answer.
func CredentialStepFor(mintOnCollect bool) PathStep {
	if mintOnCollect {
		return credentialStepMintOnCollect
	}
	return credentialStepEmailed
}

// stepVariants holds every step whose TRUTH depends on the goatnet's credential
// flow, keyed by step ID: [0] is the mint-on-collect wording, [1] the emailed.
//
// This started as a single hardcoded "credential" case, which covered the
// developer path and nothing else. Three other roles describe fetching a
// bundle in their own words — platform-admin and network-admin inside their
// "get your own access working" step, partner in its own bundle step — and none
// of them swapped. So on a mint-on-collect goatnet those three told the reader
// to fetch an emailed bundle link that is never sent, and to beat an expiry
// that does not exist. Exactly the failure the swap was built to prevent,
// applied to one of four places. Anything describing how the credential arrives
// belongs in this map.
var stepVariants = map[string][2]PathStep{
	"credential": {credentialStepMintOnCollect, credentialStepEmailed},
	"access-platform-admin": {
		{ID: "access-platform-admin", Title: "Get your own access working", Mins: 10,
			Body: "You are onboarded like anyone else and nothing below works until this does: set your password from your welcome email, and if your manifest carries a fabric leg, mint your fabric credential on the console's fabric-credential page — nothing is emailed to you.",
			Clock: &Deadline{Window: "24 hours — the login link and code come from one request and share the deadline",
				What:    "each one-time link in your welcome email",
				Recover: "ask another admin to re-send your welcome package — nothing you already hold is invalidated"}},
		{ID: "access-platform-admin", Title: "Get your own access working", Mins: 10,
			Body: "You are onboarded like anyone else and nothing below works until this does: set your password from your welcome email, and if your manifest carries a fabric leg, fetch your bundle before its link expires.",
			Clock: &Deadline{Window: "24 hours for the login link and code (one flow, one deadline); the bundle link about 24 hours and once only",
				What:    "each one-time link in your welcome email",
				Recover: "ask another admin to re-send your welcome package — nothing you already hold is invalidated"}},
	},
	"access-network-admin": {
		{ID: "access-network-admin", Title: "Get your own access working", Mins: 10,
			Body: "You are onboarded like anyone else and nothing below works until this does: set your password from your welcome email, and mint your fabric credential on the console's fabric-credential page if your manifest carries a fabric leg.",
			Clock: &Deadline{Window: "24 hours — the login link and code come from one request and share the deadline",
				What:    "each one-time link in your welcome email",
				Recover: "ask another admin to re-send your welcome package — nothing you already hold is invalidated"}},
		{ID: "access-network-admin", Title: "Get your own access working", Mins: 10,
			Body: "You are onboarded like anyone else and nothing below works until this does: set your password from your welcome email, and fetch your bundle if your manifest carries a fabric leg.",
			Clock: &Deadline{Window: "24 hours for the login link and code (one flow, one deadline); the bundle link about 24 hours and once only",
				What:    "each one-time link in your welcome email",
				Recover: "ask another admin to re-send your welcome package — nothing you already hold is invalidated"}},
	},
	"bundle-partner": {
		{ID: "bundle-partner", Title: "Mint your credential and learn the fabric", Mins: 20, Topic: "fabric",
			Body: "If your level carries a fabric identity, you mint your own certificate and key — once — on the console's fabric-credential page; nothing arrives by email. The CLI has a built-in tutor that needs no live credentials, so you can start learning before you mint.",
			Clock: &Deadline{Limit: "does not expire, but works only ONCE",
				What:    "the bundle + portal key on the fabric-credential page",
				Recover: "ask an admin to re-issue (rotate) your credential — this invalidates the previous key and bundle, so only if they are truly lost"}},
		{ID: "bundle-partner", Title: "Fetch your bundle and learn the fabric", Mins: 20, Topic: "fabric",
			Body: "If your level carries a fabric identity, your certificate and key arrive as a one-time bundle, and the CLI has a built-in tutor that needs no live credentials.",
			Clock: &Deadline{Window: "about 24 hours, and it works only ONCE",
				What:    "the bundle download link in your welcome email",
				Recover: "ask an admin to re-send your welcome package — this does not invalidate anything you already hold"}},
	},
}

// withCredentialFlow returns the role with every flow-dependent step swapped for
// the goatnet's wording. It COPIES the path slice: Roles is a package var shared
// by every request, and mutating a step in place would rewrite the guide for
// whichever goatnet asked last.
func withCredentialFlow(r Role, mintOnCollect bool) Role {
	path := make([]PathStep, len(r.Path))
	copy(path, r.Path)
	for i := range path {
		v, ok := stepVariants[path[i].ID]
		if !ok {
			continue
		}
		if mintOnCollect {
			path[i] = v[0]
		} else {
			path[i] = v[1]
		}
	}
	r.Path = path
	return r
}

// Roles is the closed set of documented views. NETWORK ADMIN IS ITS OWN VIEW,
// not an overlay on another tier: the site network-admin entitlement (ADR 1113)
// is orthogonal to onboarding tier in the identity grammar, but a reader wants
// one page about their job, and a fifth view says that far more plainly than a
// conditional block bolted onto four others.
var Roles = []Role{
	{
		Key:     "browse",
		Title:   "Browse",
		OneLine: "You can see the fabric and the surfaces, and you publish nothing.",
		Path: []PathStep{
			{Title: "Sign in once", Body: "Set your password from the link in your welcome email, then use it for every surface. There is no separate account per tool.", Mins: 5,
				Clock: &Deadline{Window: "24 hours — the login link and code come from one request and share the deadline",
					What:    "the one-time login link in your welcome email",
					Recover: "ask an admin for a fresh link; your account is not locked, the link just aged out"}},
			{Title: "Learn the picture", Body: "Panoscope is the live common operating picture — start there to see what the fabric is carrying.", Mins: 10, Topic: "surfaces"},
			{Title: "Know your edges", Body: "You have no fabric identity and no portal key, so the Portal will not admit you. That is your access level, not a fault. If your work needs to publish or inspect deeply, ask for a level change rather than a key.", Mins: 2},
		},
		Topics: []string{"credentials", "surfaces", "utilities", "troubleshooting", "firewall", "windows", "gotchas"},
		Repos:  []string{"hello-galaxy-zenoh-patterns", "hello-galaxy-time-hygiene"},
	},
	{
		Key:     "developer",
		Title:   "Developer",
		OneLine: "You hold a fabric identity and publish under your own namespace.",
		Path: []PathStep{
			{Title: "Sign in once", Body: "Set your password from your welcome email. It is single sign-on for every surface except the Portal, which takes your portal API key.", Mins: 5,
				Clock: &Deadline{Window: "24 hours — the login link and code come from one request and share the deadline",
					What:    "the one-time login link in your welcome email",
					Recover: "ask an admin for a fresh link; your account is not locked, the link just aged out"}},
			credentialStepMintOnCollect,
			{Title: "Install the CLI", Body: "The get page gives you the one-line install for your OS, and `goat tutor` teaches the fabric against a built-in sandbox before you touch anything live.", Mins: 15, Topic: "cli"},
			{Title: "Publish under your own namespace", Body: "Writes outside your namespace are refused by design. Your write anchor is shown in the Portal under your identity.", Mins: 30, Topic: "fabric"},
			{Title: "Work from a reference", Body: "The catalog holds runnable references for the patterns you are most likely to need. They encode constraints that are easy to get subtly wrong, so reading one is usually faster than rediscovering why the fabric refused something.", Mins: 20, Topic: "repos"},
			{Title: "Bringing your own equipment on", Body: "If you own gear that will publish, you enroll it yourself against a quota an operator grants — no review queue. Choose its class by what it DOES: that choice derives its network reach automatically and is enforced from then on. Retire it the same way when it is done, which also returns your quota.", Mins: 20, Topic: "machines"},
		},
		Topics: []string{"credentials", "cli", "fabric", "machines", "surfaces", "repos", "utilities", "troubleshooting", "firewall", "windows", "gotchas"},
		Repos: []string{
			"hello-galaxy-zenoh-patterns", "hello-galaxy-time-hygiene",
			"hello-galaxy-grant-encryption", "hello-galaxy-partner-data-sharing",
			// A developer IS the custodian in the machine flow: function-class
			// enrollment is custodian self-service (ADR 1096 decision #1), so the
			// person who actually runs `goat machine enroll` for their own gear
			// sits in this tier, not in iam-admin. Granting the walkthrough only
			// to the admin tiers gave it to the people who supervise the act and
			// withheld it from the people who perform it.
			"hello-galaxy-human-machine-onboarding",
		},
	},
	{
		Key:     "iam-admin",
		Title:   "Identity admin",
		OneLine: "You bring people onto the goatnet and decide what they may reach.",
		Path: []PathStep{
			{Title: "Your own access first", Body: "You are onboarded like anyone else — password, and a portal key if your manifest carries a fabric leg. Do that before you administer anyone.", Mins: 10},
			{Title: "Invite on the rails", Body: "Onboarding is delegated in three stages so no single person both invites and admits. Use the console; a hand-made identity looks the same and behaves differently.", Mins: 20, Topic: "identity-admin"},
			{Title: "Sponsor within your own ceiling", Body: "You may not sponsor above your own level. The gate is fail-closed and will refuse rather than silently downgrade.", Mins: 5, Topic: "identity-admin"},
			{Title: "Know what a credential is worth", Body: "Four different secrets reach a new person by three different routes. Knowing which is which is most of the job.", Mins: 5, Topic: "credentials"},
			{Title: "Set a machine allowance while you are onboarding them", Body: "Equipment is enrolled by the person who owns it, against a standing allowance you grant — there is no review queue and you are not approving individual machines. Set it as part of bringing someone on. A custodian still sitting at zero when their gear arrives is the case that produces a hand-assigned joining key, and that is the one enrolment nobody can account for afterwards.", Mins: 5, Topic: "machines"},
			{Title: "Change access, do not hand out keys", Body: "When someone cannot reach a surface, the fix is almost always their access level — not minting them a credential by hand.", Mins: 5, Topic: "gotchas"},
		},
		Topics: []string{"identity-admin", "credentials", "machines", "gotchas", "surfaces", "network-health", "repos", "utilities", "troubleshooting", "firewall", "windows"},
		Repos: []string{
			"hello-galaxy-human-machine-onboarding", "hello-galaxy-access-mgmt",
			"hello-galaxy-mesh-change-control",
			"hello-galaxy-universal-id", "hello-galaxy-partner-data-sharing",
			"hello-galaxy-time-hygiene",
		},
	},
	{
		Key:     "platform-admin",
		Title:   "Platform admin",
		OneLine: "You run the goatnet itself: hosts, deploys and the substrate under everyone else.",
		Path: []PathStep{
			// Wording swaps per goatnet — see stepVariants.
			{ID: "access-platform-admin", Title: "Get your own access working", Mins: 10},
			{Title: "Send your SSH public key", Body: "Host access is NOT set up for you automatically. The onboarding ceremony skips it silently when it has no public key from you, and only you can supply one. Submit it and wait for approval — a submitted key grants nothing until an admin says yes.", Mins: 10, Topic: "operator"},
			{Title: "Request your operator key — it is a SECOND key", Body: "Your operator key is not in your bundle and not in your email, and it does not replace the scoped vp_ key that signs you into the Portal. Request it on the console's operator-key page; an admin approves and you collect it there once.", Mins: 5, Topic: "operator"},
			{Title: "Learn what the destructive commands really do", Body: "Several admin commands are safe-sounding and are not. Read this before you run one to help somebody.", Mins: 10, Topic: "gotchas"},
			{Title: "Deploy by the ledger", Body: "Image pins are governed and every deploy records a rollback. Never bump a role default to move a goatnet — it is invisible to anyone reading that goatnet's config.", Mins: 20, Topic: "operator"},
			{Title: "Know how to answer whether it is the network", Body: "You have the relay-health dashboard and most people do not. When someone reports an outage, you are the one who can tell whether transport is actually degraded — and the one who has to avoid mistaking one unhappy relay for an outage.", Mins: 10, Topic: "network-health"},
			{Title: "Learn the credential map before you hand anything out", Body: "Four different secrets reach a new person by three different routes. You will be asked to fix access constantly, and most of those requests are a mesh problem or a level problem rather than a missing key — telling them apart is most of the job.", Mins: 5, Topic: "credentials"},
			{Title: "Bring people on without going around the rails", Body: "Onboarding is delegated across three stages so no single person both invites and admits, and you can only sponsor up to your own level. An identity made by hand looks right and behaves differently: it has a login and nothing else, which fails at the moment the person tries to work.", Mins: 20, Topic: "identity-admin"},
			{Title: "Grant machine allowances, and read the audit that checks them", Body: "Custodians enroll their own equipment against an allowance you grant them once — grant it up front, because the gap between gear arriving and an allowance existing is where an ungoverned enrollment happens. A live audit then reads mesh membership against the machine registry and names any peer holding a gear-class group that no record accounts for. Most of what it names is a peer whose name drifted from its handle, not a side door: rename the peer to match, never add an exception.", Mins: 10, Topic: "machines"},
			{Title: "Fix access by changing the level, not by minting", Body: "When somebody cannot reach a surface, change what their level grants — that mints what the level implies and leaves the registry telling the truth. Hand-issuing a credential leaves the record saying one thing and reality another.", Mins: 5, Topic: "gotchas"},
		},
		Topics: []string{"operator", "network-health", "identity-admin", "credentials", "machines", "gotchas", "cli", "fabric", "surfaces", "repos", "utilities", "troubleshooting", "firewall", "windows"},
		Repos: []string{
			"hello-galaxy-human-machine-onboarding", "hello-galaxy-zagros-pod",
			"hello-galaxy-spire-spiffe-patterns", "hello-galaxy-access-mgmt",
			"hello-galaxy-mesh-change-control", "hello-galaxy-time-hygiene",
			"hello-galaxy-goatnet-from-scratch",
		},
	},
	{
		Key:     "network-admin",
		Title:   "Network admin",
		OneLine: "You are an identity admin who also brings the transport gear on — the routers, switches and radios the mesh rides on — and you can see how the network is behaving across the goatnet.",
		Path: []PathStep{
			// Wording swaps per goatnet — see stepVariants.
			{ID: "access-network-admin", Title: "Get your own access working", Mins: 10},
			{Title: "Know what your role is made of", Body: "Network admin is not a separate role. You hold the identity-admin tier PLUS a group that says which site's gear you may enroll. So everything an identity admin does, you can do — and the enrollment of network gear is what the site group adds on top.", Mins: 5, Topic: "network"},
			{Title: "Do the identity-admin work too", Body: "Bringing people on, sponsoring within your ceiling, and knowing which of the four credentials someone is actually missing. You will be asked all of it, because your tier is the one that answers it.", Mins: 25, Topic: "identity-admin"},
			{Title: "Check which sites you cover", Body: "Your reach comes from which site group or groups you hold — one site, several, or the whole fleet. It is not implied by the role, so check rather than assume.", Mins: 5, Topic: "network"},
			{Title: "Enroll transport gear, and know its rules are about the GEAR", Body: "A router or radio joins as a NETWORK-class device, and that class is deliberately the most restricted thing on the mesh. Those restrictions describe the DEVICE, not you — read them before your first enrollment so the refusals make sense.", Mins: 20, Topic: "network"},
			{Title: "Answer whether it is the network", Body: "Your tier can open the relay-health dashboard, so you are one of the few people who can tell whether transport is genuinely degraded when somebody reports an outage — anywhere on the goatnet, not only at your own sites.", Mins: 10, Topic: "network-health"},
			{Title: "Never widen a device to make a problem go away", Body: "Giving a bearer device fabric access is a separate, deliberate decision and never a side effect of enrolment. If something seems to need it, that is a conversation.", Mins: 3, Topic: "gotchas"},
		},
		Topics: []string{"network", "network-health", "identity-admin", "credentials", "machines", "gotchas", "surfaces", "utilities", "troubleshooting", "firewall", "windows", "repos"},
		Repos:  []string{"hello-galaxy-zagros-pod", "hello-galaxy-time-hygiene"},
	},
	{
		// PARTNER IS AFFILIATION, NOT A TIER. The identity grammar keeps those
		// orthogonal (a partner-org member still holds browse / developer / an
		// admin tier), and this view exists for the same reason network-admin's
		// does: a reader wants one page about their situation, and "read the
		// developer path plus scattered caveats" is not that page. The view says
		// so in its own first step rather than pretending to be a sixth tier.
		Key:     "partner",
		Title:   "Partner",
		OneLine: "Your organisation is a foreign government or military organisation on this goatnet. Your access level still decides what you may do — this page is what is different where data crosses organisation lines.",
		Path: []PathStep{
			{Title: "Get onboarded like anyone else", Body: "A sponsor invites you, you submit your own details at the intake page, and an admin approves before anything is minted. Nobody else fills in your record for you — that is the design, not bureaucracy.", Mins: 10,
				Clock: &Deadline{Window: "24 hours — the login link and code come from one request and share the deadline",
					What:    "the one-time login link in your welcome email",
					Recover: "ask your sponsor or an admin for a fresh link; your account is not locked, the link just aged out"}},
			{Title: "Partner is who you are, not a permission level", Body: "Your organisation is recorded on your identity. Your access level — browse, developer, or an admin tier — is a separate thing and is what surfaces check. Read the path for your level too; this page only adds what is different for a foreign government or military organisation.", Mins: 3},
			// Wording swaps per goatnet — see stepVariants.
			{ID: "bundle-partner", Title: "Fetch your bundle and learn the fabric", Mins: 20, Topic: "fabric"},
			{Title: "Know your data room", Body: "You publish under your own namespace, and data crosses between organizations only through release lanes somebody deliberately opened. Read this before you publish anything sensitive.", Mins: 10, Topic: "partner-data"},
			{Title: "Share through lanes, never by workaround", Body: "Releasing data to another organization, or publishing into a shared lane, is a grant made by the goatnet's admins. If a lane you expect is missing, ask — do not paste data somewhere reachable as a workaround.", Mins: 5, Topic: "partner-data"},
			{Title: "Bring your organization's machines properly", Body: "Machines enroll against your own custodian identity, under a quota an operator grants. A refusal usually means the quota or the class is doing its job.", Mins: 15, Topic: "partner-data"},
			{Title: "When something seems missing, ask", Body: "The fabric refuses quietly by design — an empty subscription can mean no data, or no access, and they look the same from your side. Ask before debugging for an afternoon.", Mins: 3, Topic: "troubleshooting"},
		},
		Topics: []string{"partner-data", "fabric", "machines", "cli", "surfaces", "credentials", "utilities", "troubleshooting", "firewall", "windows", "gotchas", "repos"},
		Repos: []string{
			"hello-galaxy-partner-data-sharing", "zagros-moon-pod",
			"hello-galaxy-zenoh-patterns", "hello-galaxy-grant-encryption",
			"hello-galaxy-time-hygiene",
		},
	},
}

// TotalMins is how long the whole path takes to work through, summed from its
// steps. Deep dives are deliberately NOT counted: each card advertises its own
// cost, and folding optional reading into a headline number would overstate what
// the path itself asks for.
func (r Role) TotalMins() int {
	t := 0
	for _, s := range r.Path {
		t += s.Mins
	}
	return t
}

// HumanMins renders a minute count the way a person would say it.
func HumanMins(m int) string {
	switch {
	case m <= 0:
		return ""
	case m < 60:
		return fmt.Sprintf("about %d minutes", m)
	case m%60 == 0:
		h := m / 60
		if h == 1 {
			return "about an hour"
		}
		return fmt.Sprintf("about %d hours", h)
	default:
		return fmt.Sprintf("about %d h %d min", m/60, m%60)
	}
}

// RoleByKey resolves a role, degrading to browse. An unknown or hand-edited
// value must never show MORE than the least-privileged view.
func RoleByKey(key string) Role {
	k := strings.ToLower(strings.TrimSpace(key))
	// Accept the tier spellings the rest of the system uses.
	switch k {
	case "network_admin", "network admin":
		k = "network-admin"
	case "iam_admin":
		k = "iam-admin"
	case "platform_admin":
		k = "platform-admin"
	}
	for _, r := range Roles {
		if r.Key == k {
			return r
		}
	}
	return Roles[0]
}

// RepoRef is one catalog entry.
//
// DELIBERATELY NOT A URL. Repositories get renamed and move between
// organizations — that is a live migration here, not a hypothetical — and a page
// full of hardcoded links rots into a page full of 404s that quietly teaches
// readers the docs are stale. So an entry carries a SLUG and a description, and
// the link is composed at render time from a configured base. With no base
// configured the entry still renders, by name, with instructions to ask.
type RepoRef struct {
	Slug  string
	Title string
	What  string   // what a reader gets from it, in one sentence
	Roles []string // role keys this is worth the time of

	// Badges. NOTE the dimension differs from a topic's on purpose: these do not
	// sort into essential and optional. They are best-practice references for
	// DIFFERENT CONCERNS, and which one matters depends entirely on what you are
	// building today — ranking them by importance would assert a priority that
	// does not exist and quietly discourage reading the "lesser" ones.
	//
	// So the badge is the CONCERN. Time still applies, and DoMin matters more
	// here than anywhere else: these are runnable, and "clone it and watch it
	// work" is a different commitment from "read the README".
	Category string
	ReadMin  int
	DoMin    int
}

// Catalog is the hello-galaxy series — small, runnable, self-contained
// demonstrations — plus the community product repos those demonstrations lead
// into, when the repo is genuinely open to build against (the moon-pod repo is;
// the partner-showcase kit is NOT listed because its governance distributes a
// release zip, never repo access). Ordered roughly from on-ramp to specialist.
//
// EVERY ENTRY MUST BE A REPO A READER CAN ACTUALLY BE GIVEN. GitHub refuses to
// add a collaborator to an archived repo, so an archived entry can never be in
// the grant lane and sends readers to a 404. Remove it rather than annotate it:
// a card that says "you cannot open this" is not an on-ramp. This list and the
// grant rules change together, because the two have to agree or the catalog
// lies.
var Catalogue = []RepoRef{ //nolint:misspell // keep "Catalogue" as the exported name used in docs
	{Slug: "hello-galaxy-zenoh-patterns", Title: "Fabric usage patterns",
		What:     "When one topic is right and when it is not, how to design key expressions you can actually retrieve by, and latest-vs-history storage.",
		Roles:    []string{"developer", "browse"},
		Category: "Data fabric", ReadMin: 10, DoMin: 30},
	{Slug: "hello-galaxy-time-hygiene", Title: "Time hygiene",
		What:     "Why participants must agree with EACH OTHER about time rather than each be right about UTC, the two readings that answers, and how a signed clock verdict keeps a lying peer from dragging the fleet. Five of its six scenarios need no infrastructure at all.",
		Roles:    []string{"developer", "platform-admin", "network-admin", "iam-admin", "browse", "partner"},
		Category: "Timekeeping", ReadMin: 10, DoMin: 25},
	{Slug: "hello-galaxy-human-machine-onboarding", Title: "Human & machine onboarding",
		What:     "Onboard a person, then a machine, then a platform that decomposes into workloads — the sibling ceremonies under one identity grammar, plus the day-2 admin page.",
		Roles:    []string{"iam-admin", "platform-admin"},
		Category: "Onboarding", ReadMin: 12, DoMin: 40},
	{Slug: "hello-galaxy-access-mgmt", Title: "Access management, live",
		What:     "Rotate a live actor in place, and detect and revoke a flooding one, on a real fabric.",
		Roles:    []string{"iam-admin", "platform-admin"},
		Category: "Access control", ReadMin: 8, DoMin: 25},
	{Slug: "hello-galaxy-mesh-change-control", Title: "Changing network access",
		What:     "The NetBird dashboard is for looking: who holds which group, and who may reach what, changes through a reviewed diff that a machine applies. Predict what that machine will do, see what review refuses and why, and rehearse the pull request. Five of its six scenarios need no infrastructure at all.",
		Roles:    []string{"platform-admin", "iam-admin", "developer"},
		Category: "Network access", ReadMin: 10, DoMin: 30},
	{Slug: "hello-galaxy-universal-id", Title: "Universal identity",
		What:     "One portable identity descriptor enforced coherently across different platforms.",
		Roles:    []string{"iam-admin"},
		Category: "Identity", ReadMin: 12, DoMin: 30},
	{Slug: "hello-galaxy-grant-encryption", Title: "Grant-keyed encryption",
		What:     "How a data owner restricts a stream to an audience by encrypting to it, with nothing in the read path able to decrypt.",
		Roles:    []string{"developer"},
		Category: "Data protection", ReadMin: 12, DoMin: 35},
	{Slug: "hello-galaxy-partner-data-sharing", Title: "Partner data sharing",
		What:     "Release data to one partner or a whole coalition so they see only what was released, on a deny-by-default fabric.",
		Roles:    []string{"developer", "iam-admin"},
		Category: "Partner sharing", ReadMin: 10, DoMin: 30},
	{Slug: "hello-galaxy-spire-spiffe-patterns", Title: "SPIRE / SPIFFE federation",
		What:     "Runnable proof that two trust domains can do cross-domain mTLS.",
		Roles:    []string{"platform-admin"},
		Category: "Trust federation", ReadMin: 10, DoMin: 40},
	{Slug: "hello-galaxy-goatnet-from-scratch", Title: "A goatnet from scratch",
		What:     "The worked example behind the goatnet tutorials: a founder site, then its first federated node, each rehearsed with goatbuilder against a recorded checkpoint before the real run. Nothing in it touches a network.",
		Roles:    []string{"platform-admin"},
		Category: "Standing up a goatnet", ReadMin: 10, DoMin: 20},
	{Slug: "hello-galaxy-zagros-pod", Title: "Stand up a pod",
		What:     "A runnable moon-pod standup and bidirectional peering smoke test, driven by a central registry.",
		Roles:    []string{"platform-admin", "network-admin"},
		Category: "Standing up a pod", ReadMin: 10, DoMin: 60},
	{Slug: "zagros-moon-pod", Title: "The moon-pod itself",
		What:     "The partner-custodial appliance for two-way data exchange with the fabric — run on your own hardware, in your facility, under your custody. This is the open co-development surface: self-contained, no internal credentials, safe to build against.",
		Roles:    []string{"platform-admin", "network-admin", "iam-admin"},
		Category: "Partner sharing", ReadMin: 10},
}

// ReposFor returns the catalog entries a role should look at, in catalog
// order, using the role's own preference list first.
func ReposFor(role Role) []RepoRef {
	seen := map[string]bool{}
	var out []RepoRef
	for _, slug := range role.Repos {
		for _, r := range Catalogue { //nolint:misspell // keep "Catalogue" as the exported name used in docs
			if r.Slug == slug && !seen[slug] {
				out = append(out, r)
				seen[slug] = true
			}
		}
	}
	for _, r := range Catalogue { //nolint:misspell // keep "Catalogue" as the exported name used in docs
		if seen[r.Slug] {
			continue
		}
		for _, rk := range r.Roles {
			if rk == role.Key {
				out = append(out, r)
				seen[r.Slug] = true
				break
			}
		}
	}
	return out
}

// OtherReposFor returns the rest of the catalog — every entry ReposFor did
// not pick — in catalog order. The repos page renders these under their own
// heading rather than omitting them: the series is community-wide, and a role
// filter that dropped the rest outright taught readers the missing repos did
// not exist. Someone on the identity-admin view went looking for the fabric
// usage patterns and the pod standup and found no mention of either.
func OtherReposFor(role Role) []RepoRef {
	seen := map[string]bool{}
	for _, r := range ReposFor(role) {
		seen[r.Slug] = true
	}
	var out []RepoRef
	for _, r := range Catalogue { //nolint:misspell // keep "Catalogue" as the exported name used in docs
		if !seen[r.Slug] {
			out = append(out, r)
		}
	}
	return out
}
