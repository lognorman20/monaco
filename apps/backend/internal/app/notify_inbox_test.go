package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/apns"
)

func deviceToken(label string) string {
	return fmt.Sprintf("%064x", []byte(label))[:64]
}

func TestNotificationService_listPagesNewestFirstWithUnreadCount(t *testing.T) {
	// Arrange: five notifications a second apart.
	c := newNotifyCabal(t, "Jordan")
	inbox := NewNotificationService(c.h.Store, c.h.Privy)
	ctx := context.Background()
	base := c.now
	for i := 0; i < 5; i++ {
		c.now = base.Add(time.Duration(10+i) * time.Second)
		c.notifier.Notify(ctx, []string{c.members[0]}, Notification{Kind: NotifyFundCredited, Title: fmt.Sprintf("n%d", i)})
	}

	// Act
	first, err := inbox.List(ctx, c.tokens[0], "", 3)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	second, err := inbox.List(ctx, c.tokens[0], first.NextCursor, 3)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}

	// Assert
	titles := func(p NotificationsPage) string {
		var out []string
		for _, it := range p.Items {
			out = append(out, it.Title)
		}
		return strings.Join(out, ",")
	}
	if titles(first) != "n4,n3,n2" || first.NextCursor == "" {
		t.Fatalf("first page = %s (cursor %q)", titles(first), first.NextCursor)
	}
	if titles(second) != "n1,n0" || second.NextCursor != "" {
		t.Fatalf("second page = %s (cursor %q)", titles(second), second.NextCursor)
	}
	if first.UnreadCount != 5 || first.Items[0].Category != NotifyCategoryMoney || first.Items[0].ReadAt != nil {
		t.Fatalf("first page meta = %+v", first)
	}
}

func TestNotificationService_markRead(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	inbox := NewNotificationService(c.h.Store, c.h.Privy)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		c.notifier.Notify(ctx, []string{c.members[1]}, Notification{Kind: NotifyFundCredited, Title: fmt.Sprintf("n%d", i)})
	}
	page, _ := inbox.List(ctx, c.tokens[1], "", 10)
	jordansPage, _ := inbox.List(ctx, c.tokens[0], "", 10)

	// Priya's own id is read; Jordan's id in the same request is ignored.
	left, err := inbox.MarkRead(ctx, c.tokens[1], []string{page.Items[0].ID, jordansPage.Items[0].ID}, false)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if left != 2 {
		t.Fatalf("unread after one = %d, want 2", left)
	}
	if again, _ := inbox.List(ctx, c.tokens[0], "", 10); again.UnreadCount != jordansPage.UnreadCount {
		t.Fatal("another member's notification was marked read")
	}
	left, err = inbox.MarkRead(ctx, c.tokens[1], nil, true)
	if err != nil || left != 0 {
		t.Fatalf("mark all = %d, %v", left, err)
	}
	after, _ := inbox.List(ctx, c.tokens[1], "", 10)
	if after.Items[0].ReadAt == nil {
		t.Fatal("readAt not set")
	}

	for name, call := range map[string]func() error{
		"neither ids nor all": func() error { _, err := inbox.MarkRead(ctx, c.tokens[1], nil, false); return err },
		"both ids and all":    func() error { _, err := inbox.MarkRead(ctx, c.tokens[1], []string{page.Items[0].ID}, true); return err },
		"a bad id":            func() error { _, err := inbox.MarkRead(ctx, c.tokens[1], []string{"nope"}, false); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidNotificationRead) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestNotificationService_listRejectsBadInput(t *testing.T) {
	c := newNotifyCabal(t, "Jordan")
	inbox := NewNotificationService(c.h.Store, c.h.Privy)
	if _, err := inbox.List(context.Background(), c.tokens[0], "", 0); !errors.Is(err, ErrInvalidNotificationLimit) {
		t.Fatalf("limit 0: %v", err)
	}
	if _, err := inbox.List(context.Background(), c.tokens[0], "", 101); !errors.Is(err, ErrInvalidNotificationLimit) {
		t.Fatalf("limit 101: %v", err)
	}
	if _, err := inbox.List(context.Background(), c.tokens[0], "!!", 10); !errors.Is(err, ErrInvalidNotificationCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestNotificationService_devices(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	inbox := NewNotificationService(c.h.Store, c.h.Privy)
	ctx := context.Background()
	token := deviceToken("phone-1")

	cases := []struct {
		name     string
		token    string
		platform string
		appEnv   string
		want     error
	}{
		{name: "too short", token: "abc", platform: "ios", appEnv: "debug", want: ErrInvalidDeviceToken},
		{name: "not hex", token: strings.Repeat("z", 64), platform: "ios", appEnv: "debug", want: ErrInvalidDeviceToken},
		{name: "android", token: token, platform: "android", appEnv: "debug", want: ErrInvalidDevicePlatform},
		{name: "unknown env", token: token, platform: "ios", appEnv: "staging", want: ErrInvalidDeviceAppEnv},
		{name: "valid, upper case", token: strings.ToUpper(token), platform: "ios", appEnv: "production"},
	}
	for _, tc := range cases {
		if err := inbox.RegisterDevice(ctx, c.tokens[0], tc.token, tc.platform, tc.appEnv); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	devices, _ := c.h.Store.ListDeviceTokensForUsers(ctx, c.members)
	if len(devices) != 1 || devices[0].Token != token || devices[0].UserID != c.members[0] || devices[0].AppEnv != "production" {
		t.Fatalf("devices = %+v", devices)
	}

	// Priya signs in on the same phone: the token moves to her.
	if err := inbox.RegisterDevice(ctx, c.tokens[1], token, "ios", "production"); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	devices, _ = c.h.Store.ListDeviceTokensForUsers(ctx, c.members)
	if len(devices) != 1 || devices[0].UserID != c.members[1] {
		t.Fatalf("token did not move: %+v", devices)
	}

	// Jordan cannot remove Priya's token; Priya can.
	_ = inbox.UnregisterDevice(ctx, c.tokens[0], token)
	if devices, _ = c.h.Store.ListDeviceTokensForUsers(ctx, c.members); len(devices) != 1 {
		t.Fatal("another member removed the token")
	}
	if err := inbox.UnregisterDevice(ctx, c.tokens[1], token); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if devices, _ = c.h.Store.ListDeviceTokensForUsers(ctx, c.members); len(devices) != 0 {
		t.Fatalf("token still there: %+v", devices)
	}
}

func TestUpsertDeviceToken_keepsTheNewestTen(t *testing.T) {
	c := newNotifyCabal(t, "Jordan")
	ctx := context.Background()
	start := time.Now().UTC()
	for i := 0; i < 12; i++ {
		if err := c.h.Store.UpsertDeviceToken(ctx, c.members[0], deviceToken(fmt.Sprintf("device-%02d", i)), "ios", "debug", start.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	devices, _ := c.h.Store.ListDeviceTokensForUsers(ctx, c.members)
	if len(devices) != 10 {
		t.Fatalf("devices = %d, want 10", len(devices))
	}
	for _, d := range devices {
		if d.Token == deviceToken("device-00") || d.Token == deviceToken("device-01") {
			t.Fatal("an old device survived the cap")
		}
	}
}

func testAPNsClient(t *testing.T, server *httptest.Server) *apns.Client {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	client, err := apns.NewClient(apns.Config{
		KeyID: "KEY", TeamID: "TEAM", BundleID: "com.monaco.app", Env: apns.EnvSandbox,
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	if err != nil {
		t.Fatalf("apns client: %v", err)
	}
	return client.WithEndpoints(server.Client(), server.URL+"/production", server.URL+"/sandbox")
}

func TestAPNsPushSender_deliversToEachDeviceAndForgetsUnregisteredOnes(t *testing.T) {
	// Arrange: Jordan has a live debug phone and a dead production phone.
	c := newNotifyCabal(t, "Jordan")
	ctx := context.Background()
	live, dead := deviceToken("live-phone"), deviceToken("dead-phone")
	if err := c.h.Store.UpsertDeviceToken(ctx, c.members[0], live, "ios", "debug", time.Now()); err != nil {
		t.Fatalf("register live: %v", err)
	}
	if err := c.h.Store.UpsertDeviceToken(ctx, c.members[0], dead, "ios", "production", time.Now()); err != nil {
		t.Fatalf("register dead: %v", err)
	}
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, dead) {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sender := NewAPNsPushSender(testAPNsClient(t, server), c.h.Store)

	// Act
	sender.Send(ctx, []PushMessage{{UserID: c.members[0], NotificationID: "n1", Kind: NotifyFundCredited, Title: "$5 is in the pot", Badge: 1}})

	// Assert
	if len(paths) != 2 {
		t.Fatalf("requests = %v, want one per device", paths)
	}
	want := map[string]bool{"/sandbox/3/device/" + live: true, "/production/3/device/" + dead: true}
	for _, p := range paths {
		if !want[p] {
			t.Fatalf("unexpected request %s; a device goes to the host of the build that registered it", p)
		}
	}
	devices, _ := c.h.Store.ListDeviceTokensForUsers(ctx, c.members)
	if len(devices) != 1 || devices[0].Token != live {
		t.Fatalf("devices after 410 = %+v, want only the live one", devices)
	}
}
