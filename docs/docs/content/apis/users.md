# API / Users

| Method | Endpoint                                                     | Description                                      |
| :----- | :----------------------------------------------------------- | :----------------------------------------------- |
| GET    | [/api/users/{user_id}/integration-tokens](#get-apiusersuser_idintegration-tokens) | List integration bearer tokens for an API user.  |
| POST   | [/api/users/{user_id}/integration-tokens](#post-apiusersuser_idintegration-tokens) | Create a new integration bearer token.           |
| DELETE | [/api/users/{user_id}/integration-tokens/{token_id}](#delete-apiusersuser_idintegration-tokenstoken_id) | Revoke an integration bearer token.              |

______________________________________________________________________

#### GET /api/users/{user_id}/integration-tokens

List integration bearer tokens for an API user. The plaintext token value is never returned from this endpoint.

##### Example Request

```shell
curl -H "Authorization: Bearer lmit_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  'http://localhost:9000/api/users/2/integration-tokens'
```

##### Example Response

```json
{
  "data": [
    {
      "id": 3,
      "user_id": 2,
      "name": "openclaw-prod",
      "last_used_at": "2026-03-23T14:30:00.000000+08:00",
      "revoked_at": null,
      "created_at": "2026-03-23T14:00:00.000000+08:00",
      "updated_at": "2026-03-23T14:30:00.000000+08:00"
    }
  ]
}
```

______________________________________________________________________

#### POST /api/users/{user_id}/integration-tokens

Create a new integration bearer token for an API user. The plaintext token is returned only once in the creation response.

##### Parameters

| Name | Type   | Required | Description                   |
| :--- | :----- | :------- | :---------------------------- |
| name | string | Yes      | Friendly name for the token.  |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/users/2/integration-tokens' \
  -H 'Content-Type: application/json' \
  -d '{"name":"openclaw-prod"}'
```

##### Example Response

```json
{
  "data": {
    "id": 3,
    "user_id": 2,
    "name": "openclaw-prod",
    "last_used_at": null,
    "revoked_at": null,
    "created_at": "2026-03-23T14:00:00.000000+08:00",
    "updated_at": "2026-03-23T14:00:00.000000+08:00",
    "token": "lmit_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  }
}
```

______________________________________________________________________

#### DELETE /api/users/{user_id}/integration-tokens/{token_id}

Revoke an integration bearer token. Revoked tokens stop working immediately.

##### Example Request

```shell
curl -u "api_user:token" -X DELETE \
  'http://localhost:9000/api/users/2/integration-tokens/3'
```

##### Example Response

```json
{
  "data": true
}
```
