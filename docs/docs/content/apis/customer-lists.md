# CustomerLists API

Customer lists are exposed under `/api/customer-lists`. In addition to private
and public subscription lists, the API supports first-level public pools
(`pool`) and organization segments (`pool_segment`). See [Public pools](pools.md)
for masking, assignment, exclusions, merge, and internal reply mailbox rules.

Authenticated responses are scoped by the active workspace selected through
`X-Listmonk-Organization-ID`. In an organization workspace, ordinary lists
from the personal workspace or another organization are not returned; in the
personal workspace, only the caller's personal lists are returned. Authorized
public pools remain an explicit cross-workspace delivery/import exception.
