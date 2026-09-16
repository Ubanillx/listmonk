# API / Templates

| Method | Endpoint | Description |
| :----- | :------- | :---------- |
| GET | [/api/templates](#get-apitemplates) | Retrieve all templates |
| GET | [/api/templates/{template_id}](#get-apitemplatestemplate_id) | Retrieve a template |
| GET | [/api/templates/{template_id}/preview](#get-apitemplatestemplate_idpreview) | Retrieve template HTML preview |
| POST | [/api/templates](#post-apitemplates) | Create a template |
| POST | [/api/templates/{template_id}/clone](#post-apitemplatestemplate_idclone) | Clone a template |
| POST | /api/templates/preview | Render and preview a template |
| PUT | [/api/templates/{template_id}](#put-apitemplatestemplate_id) | Update a template |
| PUT | [/api/templates/{template_id}/default](#put-apitemplatestemplate_iddefault) | Set default template |
| DELETE | [/api/templates/{template_id}](#delete-apitemplatestemplate_id) | Delete a template |

______________________________________________________________________

#### GET /api/templates

Retrieve all templates.

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/templates'
```

##### Example Response

```json
{
    "data": [
        {
            "id": 1,
            "created_at": "2020-03-14T17:36:41.288578+01:00",
            "updated_at": "2020-03-14T17:36:41.288578+01:00",
            "name": "Default template",
            "body": "{{ template \"content\" . }}",
            "body_source": null,
            "type": "campaign",
            "is_default": true
        }
    ]
}
```

______________________________________________________________________

#### GET /api/templates/{template_id}

Retrieve a specific template.

##### Parameters

| Name        | Type      | Required | Description                    |
|:------------|:----------|:---------|:-------------------------------|
| template_id | number    | Yes      | ID of the template to retrieve |

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/templates/1'
```

##### Example Response

```json
{
    "data": {
        "id": 1,
        "created_at": "2020-03-14T17:36:41.288578+01:00",
        "updated_at": "2020-03-14T17:36:41.288578+01:00",
        "name": "Default template",
        "body": "{{ template \"content\" . }}",
        "body_source": null,
        "type": "campaign",
        "is_default": true
    }
}
```

______________________________________________________________________

#### GET /api/templates/{template_id}/preview

Retrieve the HTML preview of a template.

##### Parameters

| Name        | Type      | Required | Description                   |
|:------------|:----------|:---------|:------------------------------|
| template_id | number    | Yes      | ID of the template to preview |

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/templates/1/preview'
```

##### Example Response

```html
<p>Hi there</p>
<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Duis et elit ac elit sollicitudin condimentum non a magna.
	Sed tempor mauris in facilisis vehicula. Aenean nisl urna, accumsan ac tincidunt vitae, interdum cursus massa.
	Interdum et malesuada fames ac ante ipsum primis in faucibus. Aliquam varius turpis et turpis lacinia placerat.
	Aenean id ligula a orci lacinia blandit at eu felis. Phasellus vel lobortis lacus. Suspendisse leo elit, luctus sed
	erat ut, venenatis fermentum ipsum. Donec bibendum neque quis.</p>

<h3>Sub heading</h3>
<p>Nam luctus dui non placerat mattis. Morbi non accumsan orci, vel interdum urna. Duis faucibus id nunc ut euismod.
	Curabitur et eros id erat feugiat fringilla in eget neque. Aliquam accumsan cursus eros sed faucibus.</p>

<p>Here is a link to <a href="https://listmonk.app" target="_blank">listmonk</a>.</p>
```

______________________________________________________________________

#### POST /api/templates

Create a template.

##### Parameters

| Name        | Type   | Required | Description                                                                   |
|:------------|:-------|:---------|:------------------------------------------------------------------------------|
| name        | string | Yes      | Name of the template                                                          |
| type        | string | Yes      | Type of the template (`campaign`, `campaign_visual`, or `tx`)                 |
| subject     | string |          | Subject line for the template (only for `tx`)                                 |
| body_source | string |          | If type is `campaign_visual`, the JSON source for the email-builder tempalate |
| body        | string | Yes      | HTML body of the template                                                     |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/templates' \
-H 'Content-Type: application/json' \
-d '{
    "name": "New template",
    "type": "campaign",
    "subject": "Your Weekly Newsletter",
    "body": "<h1>Header</h1><p>Content goes here</p>"
}'
```

##### Example Response

```json
{
    "data": [
        {
            "id": 1,
            "created_at": "2020-03-14T17:36:41.288578+01:00",
            "updated_at": "2020-03-14T17:36:41.288578+01:00",
            "name": "Default template",
            "body": "{{ template \"content\" . }}",
            "body_source": null,
            "type": "campaign",
            "is_default": true
        }
    ]
}
```

______________________________________________________________________

#### PUT /api/templates/{template_id}

Update a template.

> Refer to parameters from [POST /api/templates](#post-apitemplates)

______________________________________________________________________

#### POST /api/templates/{template_id}/clone

Clone an existing template into a new template. The source template's `type`, `body`, and `body_source` are copied. For `tx` templates, the fixed `subject` can optionally be overridden in the request.

##### Parameters

| Name        | Type   | Required | Description                                      |
|:------------|:-------|:---------|:-------------------------------------------------|
| template_id | number | Yes      | ID of the template to clone                      |
| name        | string | Yes      | Name of the new template                         |
| subject     | string |          | Optional replacement subject for `tx` templates  |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/templates/1/clone' \
-H 'Content-Type: application/json' \
-d '{
    "name": "OpenClaw Campaign Template",
    "subject": "Hello from OpenClaw"
}'
```

##### Example Response

```json
{
    "data": {
        "id": 2,
        "created_at": "2026-03-23T14:10:00.000000+08:00",
        "updated_at": "2026-03-23T14:10:00.000000+08:00",
        "name": "OpenClaw Campaign Template",
        "body": "{{ template \"content\" . }}",
        "body_source": null,
        "type": "campaign",
        "is_default": false
    }
}
```

______________________________________________________________________

#### PUT /api/templates/{template_id}/default

Set a template as the default.

##### Parameters

| Name        | Type      | Required | Description                          |
|:------------|:----------|:---------|:-------------------------------------|
| template_id | number    | Yes      | ID of the template to set as default |

##### Example Request

```shell
curl -u "api_user:token" -X PUT 'http://localhost:9000/api/templates/1/default'
```

##### Example Response

```json
{
    "data": {
        "id": 1,
        "created_at": "2020-03-14T17:36:41.288578+01:00",
        "updated_at": "2020-03-14T17:36:41.288578+01:00",
        "name": "Default template",
        "body": "{{ template \"content\" . }}",
        "body_source": null,
        "type": "campaign",
        "is_default": true
    }
}
```

______________________________________________________________________

#### DELETE /api/templates/{template_id}

Delete a template.

##### Parameters

| Name        | Type      | Required | Description                  |
|:------------|:----------|:---------|:-----------------------------|
| template_id | number    | Yes      | ID of the template to delete |

##### Example Request

```shell
curl -u "api_user:token" -X DELETE 'http://localhost:9000/api/templates/35'
```

##### Example Response

```json
{
    "data": true
}
```


## Name fallback

Template create/update requests and template responses support `name_fallback`:

```json
{
  "name_fallback": {
    "enabled": true,
    "value": "Sir or Madam",
    "invalid_values": ["N/A", "未知", "-"]
  }
}
```

An omitted or null field preserves the saved setting on update; `enabled: false` disables it. A nonblank, single-line value of at most 200 characters is required when enabled. `invalid_values` accepts at most 50 single-line strings of up to 200 characters. Invalid settings return HTTP 400. Matching trims whitespace and compares full values case-insensitively. Only the rendered customer name changes; customer records and delivery addresses do not.

The raw-template preview form accepts JSON-encoded `name_fallback`, plus `preview_name_mode=custom` and `preview_name` (including an empty string). These inputs affect preview only. Visual imports and template/campaign copies retain their rules. Configuration writes follow the existing template ownership/workspace permissions.
