# OAuth

Linea uses OAuth 2.0 to integrate with GitHub, GitLab, and Google for authentication and API access.

## Setup

Set these in `~/.config/linea/linea.env` or as environment variables:

| Variable | Description |
| :--- | :--- |
| `LINEA_GITHUB_CLIENT_ID` | GitHub OAuth client ID |
| `LINEA_GITHUB_CLIENT_SECRET` | GitHub OAuth client secret |
| `LINEA_GITLAB_CLIENT_ID` | GitLab OAuth client ID |
| `LINEA_GITLAB_CLIENT_SECRET` | GitLab OAuth client secret |
| `LINEA_GOOGLE_CLIENT_ID` | Google OAuth client ID |
| `LINEA_GOOGLE_CLIENT_SECRET` | Google OAuth client secret |
| `LINEA_OAUTH_ENCRYPTION_KEY` | AES-256-GCM key for encrypting stored tokens |

## Flow

1. Linea redirects you to the provider's authorization page.
2. After approval, the provider sends a code to Linea's callback URL.
3. Linea exchanges the code for an access token.

Tokens are encrypted at rest and stored in the database. Expired tokens are refreshed automatically.

## Connecting

Open the web UI, go to Settings > Connections, and follow the OAuth flow for each provider.
