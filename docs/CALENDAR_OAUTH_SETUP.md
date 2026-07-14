# Calendar OAuth Setup

This page covers the optional Google and Microsoft OAuth app settings that
unlock the calendar Connect buttons in Muesli and let users sync calendars via
OAuth. These settings are only needed for Google/Microsoft calendar sync.
CalDAV and ICS sources are configured per user in the UI and keep working
without any of these server environment variables.

For the related server configuration variables, see
[Calendar integration](CONFIGURATION.md#calendar-integration).

## Google Calendar

1. In Google Cloud Console, create or select a project.
2. Configure the OAuth consent screen for the app.
3. Create an OAuth 2.0 Client ID with application type `Web application`.
4. Add an authorized redirect URI that exactly matches the value you set in
   `MUESLI_GOOGLE_OAUTH_REDIRECT_URL`.

The redirect URI must also match the browser-facing callback route used by
Muesli. A typical production value looks like:

```text
https://muesli.example.com/api/calendar/oauth/google/callback
```

Muesli requests the Google Calendar read-only scope:

```text
https://www.googleapis.com/auth/calendar.readonly
```

Set all three Google variables on the Muesli server:

```text
MUESLI_GOOGLE_OAUTH_CLIENT_ID=...
MUESLI_GOOGLE_OAUTH_CLIENT_SECRET=...
MUESLI_GOOGLE_OAUTH_REDIRECT_URL=https://muesli.example.com/api/calendar/oauth/google/callback
```

The Google Connect button appears only when all three values are present.

## Microsoft Calendar

1. In Microsoft Entra ID, create an app registration.
2. Choose a multi-tenant app registration, or the equivalent "common"
   account type. Muesli uses the Azure AD `common` endpoint for the OAuth
   flow.
3. In the app's authentication settings, add a Web redirect URI that exactly
   matches the value you set in `MUESLI_MICROSOFT_OAUTH_REDIRECT_URL`.

The redirect URI must also match the browser-facing callback route used by
Muesli. A typical production value looks like:

```text
https://muesli.example.com/api/calendar/oauth/microsoft/callback
```

Add the Microsoft Graph delegated permission for calendar read access:

```text
Calendars.Read
```

Muesli also requests `offline_access` so it can obtain refresh tokens.

Set all three Microsoft variables on the Muesli server:

```text
MUESLI_MICROSOFT_OAUTH_CLIENT_ID=...
MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET=...
MUESLI_MICROSOFT_OAUTH_REDIRECT_URL=https://muesli.example.com/api/calendar/oauth/microsoft/callback
```

The Microsoft Connect button appears only when all three values are present.

## Verify And Troubleshoot

1. Set the provider variables.
2. Restart the Muesli server.
3. Refresh the client UI. The provider's Connect button should appear once the
   server sees all three values for that provider.

You can also check the status endpoint directly:

```text
GET /api/calendar/oauth/google/status
GET /api/calendar/oauth/microsoft/status
```

When a provider is configured, the response is:

```json
{ "configured": true }
```

Common issues:

- Redirect URI mismatch is the most common OAuth error.
- The redirect URI must match exactly, including scheme, host, path, and any
  trailing slash.
- Use `MUESLI_PUBLIC_URL` when you build the redirect URI. These redirects are
  browser-facing and should not use `MUESLI_INTERNAL_URL`.
- If the Connect button does not appear, confirm that the client ID, client
  secret, and redirect URL are all set for the same provider.

## Related Docs

- [Calendar integration](CONFIGURATION.md#calendar-integration)
