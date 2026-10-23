# API / Media

Campaign and template saves reconcile uploaded URLs in the body with their media
associations, even if the client omits `media`. An exact media ID must exist and
match its filename; legacy filename URLs must resolve unambiguously. Media must
be usable by the resource's saved owner, including its folder permissions.
Administrator inspection rights do not substitute for that authorization.
Missing or foreign private files reject the entire save. Starting/scheduling
also checks saved body and template dependencies. The sender checks static
dependencies before queueing customers, then checks each rendered message
before SMTP. An unresolved internal image, relative/blob/data image URL, or
missing MIME CID blocks delivery. Worker failures pause immediately, keep the
recipient pending, and appear under the `media` error category. External HTTP(S)
URLs are checked for valid shape, not fetched by the worker.

Email delivery uses remote links for media-library files. HTML images retain an
`<img src="https://your-domain/email-media/{token}/{filename}">` source and load
when the recipient opens the message. Other files that are not already linked
in the body appear at the end in one grouped attachment section with a file
count. Each file has its own colored type icon and original filename. For Word,
Excel, and PowerPoint files, **View** opens [Microsoft's Office Web Viewer](https://www.microsoft.com/en-us/copilot/blog/2013/04/10/office-web-viewer-view-office-documents-in-a-browser/) with
the recipient file URL; for PDF it opens the file URL directly. Other file types
offer download only. **Download** always uses the recipient file URL with
`?download=1`, which sends the original bytes as an attachment under the
original filename. The media-library URL requires authentication and is never
placed in outgoing mail. Microsoft must fetch Office files from the public
`app.root_url` to preview them. The Office Viewer link embeds the recipient URL;
opening that link (including by an automated link scanner) can share the URL
and file with Microsoft. An unavailable public URL or a Microsoft viewer
error does not affect the direct Download link. File
links already present in the body keep their placement and receive the
recipient URL. Plain-text messages keep filename and URL lines;
library files are no longer attached as binary or CID MIME parts. This applies
to campaign tests, new sends, paused/deferred campaigns resumed after upgrade,
and transactional template media. Previously delivered messages remain unchanged.

Campaigns that already started sending before the upgrade also use remote links
for their remaining recipients after pausing and resuming. The sender reloads
the existing media associations and converts legacy upload URLs and CID image
references at delivery time. There is no need to recreate the campaign, edit its
saved body, or reset its progress; already sent recipients are not sent again.

`GET /email-media/{token}/{filename}` is a file-specific recipient capability:
no login or public archive is required. Links are issued after sending-resource
authorization, survive application restarts, and can be forwarded by recipients.
They stop working when the media is deleted/replaced, pending transfer, moved to
another owner/workspace, or its organization is archived. Folder visibility
controls new sending authorization; it does not revoke an already delivered link.
Authenticated library URLs and list APIs retain their existing permissions.

The Super Admin media page has an explicit read-only platform catalog. Its
virtual categories are computed from each file's organization and owner;
original folders, ownership, object names, and campaign references are not
changed. `GET /api/media/categories?scope=all_owners` returns
`organization_id`, `organization_name`, `owner_user_id`, `owner_name`, and
`media_count` for each group. `GET /api/media?scope=all_owners` accepts optional
`organization_id` and `owner_user_id` (zero means personal/no owner) alongside
normal search and pagination. Result rows include `folder_name` and their
exact-ID file URLs carry the same scope. `GET /api/media/{id}` and the exact-ID
file URL accept this scope for read-only access. Only Super Admin sessions or
legacy administrator credentials can use it; personal API keys cannot. Normal
list, upload, folder, media-use and sending APIs do not inherit this scope.

Configure `app.root_url` with a publicly reachable HTTPS origin, route
`/email-media/` through the reverse proxy, and retain the uploaded files. Some
mail clients require the recipient to allow remote images. System data exports
and raw one-off transactional API attachments retain their original behavior.

Method | Endpoint                                             | Description
-------|------------------------------------------------------|---------------------------------
GET    | [/api/media/folders](#get-apimediafolders)           | Get visible media folders
GET    | `/api/media/categories?scope=all_owners`             | Super Admin virtual organization/owner groups
POST   | [/api/media/folders](#post-apimediafolders)          | Create a media folder
PUT    | [/api/media/folders/{folder_id}](#put-apimediafoldersfolder_id) | Rename a media folder
PUT    | [/api/media/folders/{folder_id}/move](#put-apimediafoldersfolder_idmove) | Move a media folder
DELETE | [/api/media/folders/{folder_id}](#delete-apimediafoldersfolder_id) | Delete an empty media folder
GET    | [/api/media](#get-apimedia)                          | Get uploaded media file
GET    | [/api/media/{media_id}](#get-apimediamedia_id)       | Get specific uploaded media file
POST   | [/api/media](#post-apimedia)                         | Upload media file
PUT    | [/api/media/{media_id}/folder](#put-apimediamedia_idfolder) | Move media to a folder
DELETE | [/api/media/{media_id}](#delete-apimediamedia_id)    | Delete uploaded media file

______________________________________________________________________

#### GET /api/media/folders

Get the media folders visible in the active workspace. The response includes
the folder's parent, `visibility`, `manageable`, `writable`, media count and visible child-folder count. A missing `parent_id`
means the folder is at the root of the workspace.

##### Example Response

```json
{
  "data": [
    {
      "id": 4,
      "name": "Product images",
      "visibility": "organization",
      "manageable": true,
      "writable": true,
      "parent_id": null,
      "media_count": 3,
      "child_count": 1
    }
  ]
}
```

______________________________________________________________________

#### POST /api/media/folders

Create a folder in the active workspace. Folder names are unique, ignoring
case, within the same parent. Requires `media:manage`.

##### Request Body

```json
{
  "name": "Product images",
  "parent_id": 0,
  "visibility": "organization"
}
```

Use `null` or `0` for `parent_id` to create a root folder.

`visibility` accepts `private` (Personal), `organization` (Organization), or
`global` (Everyone, signed-in users across workspaces). Omission preserves the
workspace default: private in personal space and organization in organization
space. Personal spaces cannot use `organization`. Every parent must be readable;
uploads and child creation remain confined to the selected owning workspace.
Folder media uses the folder audience, including ancestor restrictions. Archived
organizations cannot expose shared folders.

______________________________________________________________________

#### PUT /api/media/folders/{folder_id}

Edit a folder's name and optional visibility. Omitted visibility is preserved.
Provider objects and URL text remain unchanged. Requires `media:manage` and
folder ownership, or a platform admin in the selected owning workspace.

```json
{
  "name": "Campaign images",
  "visibility": "private"
}
```

______________________________________________________________________

#### PUT /api/media/folders/{folder_id}/move

Move a folder to another folder or to the workspace root. Moving a folder into
itself or one of its descendants is rejected. Requires `media:manage` and source
folder ownership (or a platform admin); the destination must be readable and in
the same workspace.

```json
{
  "parent_id": 9
}
```

Use `null` or `0` to move it to the root.

______________________________________________________________________

#### DELETE /api/media/folders/{folder_id}

Delete an empty media folder. Folders containing media or child folders return
HTTP 409; deletion is never recursive. Requires `media:manage` and folder ownership
(or a platform admin in the owning workspace).

______________________________________________________________________

#### GET /api/media

Get uploaded media files. `folder_id=0` returns only root media,
`folder_id={id}` returns media in that folder, and omitting `folder_id` keeps
the legacy behavior of returning all media visible in the active workspace.
Everyone folders and their media are visible across workspaces; other folders
and root media remain constrained to the selected workspace. Writes cannot cross
workspace boundaries, including drag-and-drop.

##### Example Request

```shell
curl -u "api_user:token" -X GET 'http://localhost:9000/api/media' \
--header 'Content-Type: multipart/form-data; boundary=--------------------------093715978792575906250298'
```

To list one folder:

```shell
curl -u "api_user:token" 'http://localhost:9000/api/media?folder_id=4'
```

##### Example Response

```json
{
    "data": [
        {
            "id": 1,
            "uuid": "ec7b45ce-1408-4e5c-924e-965326a20287",
            "filename": "Media file",
            "created_at": "2020-04-08T22:43:45.080058+01:00",
            "thumb_url": "/uploads/image_thumb.jpg",
            "uri": "/uploads/image.jpg"
        }
    ]
}
```
______________________________________________________________________

#### GET /api/media/{media_id}

Retrieve a specific media.

Media inside a folder requires access to that folder and every ancestor; the
same rule protects media file URLs. Root media retains the existing resource
policy and published template/campaign associations. Folder permission changes
do not retroactively modify media snapshots already copied into other resources.

##### Parameters

| Name          | Type      | Required | Description      |
|:--------------|:----------|:---------|:-----------------|
| media_id      | Number    | Yes      | Media ID.        |

##### Example Request

```shell
curl -u 'api_username:access_token' 'http://localhost:9000/api/media/7' 
```

##### Example Response

```json
{
  "data": 
    {
        "id": 7,
        "uuid": "62e32e97-d6ca-4441-923f-b62607000dd1",
        "filename": "ResumeB.pdf",
        "content_type": "application/pdf",
        "created_at": "2024-08-06T11:28:53.888257+05:30",
        "thumb_url": null,
        "provider": "filesystem",
        "meta": {},
        "url": "http://localhost:9000/uploads/ResumeB.pdf"
    }
}
```
______________________________________________________________________

#### POST /api/media

Upload a media file.

##### Parameters

| Field | Type      | Required | Description         |
|-------|-----------|----------|---------------------|
| file  | File      | Yes      | Media file to upload|
| folder_id | Number | No | Destination folder; omit or use `0` for the root |

##### Example Request

```shell
curl -u "api_user:token" -X POST 'http://localhost:9000/api/media' \
--header 'Content-Type: multipart/form-data; boundary=--------------------------183679989870526937212428' \
--form 'file=@/path/to/image.jpg'
```

Add `--form 'folder_id=4'` to upload into folder 4. The folder is logical, so
the returned media URL still uses the provider's existing object name.

##### Example Response

```json
{
    "data": {
        "id": 1,
        "uuid": "ec7b45ce-1408-4e5c-924e-965326a20287",
        "filename": "Media file",
        "created_at": "2020-04-08T22:43:45.080058+01:00",
        "thumb_uri": "/uploads/image_thumb.jpg",
        "uri": "/uploads/image.jpg"
    }
}
```

______________________________________________________________________

#### PUT /api/media/{media_id}/folder

Move media to a folder or to the root. The caller must be allowed to manage the
media row; being able to read an organization-shared file is not enough.

```json
{
  "folder_id": 4
}
```

Use `null` or `0` to move the media to the root.

______________________________________________________________________

#### DELETE /api/media/{media_id}

Delete an uploaded media file.

##### Parameters

| Field    | Type      | Required | Description             |
|----------|-----------|----------|-------------------------|
| media_id | number    | Yes      | ID of media file to delete |

##### Example Request

```shell
curl -u "api_user:token" -X DELETE 'http://localhost:9000/api/media/1'
```

##### Example Response

```json
{
    "data": true
}
```
