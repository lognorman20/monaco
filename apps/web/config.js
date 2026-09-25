// Where the invite page (join/index.html) reads a cabal's preview from:
// MONACO_API_BASE + "/v1/invites/<code>". Set it to the API's public https URL once there
// is one, and list https://trymonaco.xyz in the API's CORS_ALLOWED_ORIGINS. Empty means no
// preview: the page still shows the code, "Open in Monaco" and the App Store link.
window.MONACO_API_BASE = window.MONACO_API_BASE || "";
