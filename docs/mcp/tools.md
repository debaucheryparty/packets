# MCP Tools Reference

The Packets MCP server exposes the following tools to connected AI agents:

---

## Tool Reference

### 1. `packets_subspace_create`
Allocates a new persistent Subspace for a workspace, environment, and worker.

**Parameters:**
- `project_id` (*string, required*): The project identifier.
- `worker_id` (*string, optional*): Specific worker node identifier (default: `local`).
- `environment_id` (*string, optional*): Environment profile identifier (default: `default`).
- `workspace_id` (*string, optional*): Initial workspace snapshot identifier.

**Response Example:**
```json
{
  "id": "sub-e91760a84f01",
  "project_id": "mobile-app",
  "worker_id": "vps-worker",
  "environment_id": "android",
  "state": "ready"
}
```

---

### 2. `packets_subspace_list`
Lists persistent Subspaces across the cluster with optional filtering.

**Parameters:**
- `project_id` (*string, optional*): Filter by project ID.
- `owner_id` (*string, optional*): Filter by owner ID.

**Response Example:**
```json
[
  {
    "id": "sub-e91760a84f01",
    "project_id": "mobile-app",
    "worker_id": "vps-worker",
    "state": "ready",
    "created_at": "2026-09-18T15:07:03Z",
    "last_used_at": "2026-09-18T15:08:23Z"
  }
]
```

---

### 3. `packets_subspace_get`
Retrieves detailed state and metadata for a specific Subspace.

**Parameters:**
- `id` (*string, required*): The Subspace identifier (`sub-xxxxxx`).

---

### 4. `packets_subspace_destroy`
Destroys a Subspace and releases its associated worker allocation.

**Parameters:**
- `id` (*string, required*): The Subspace identifier (`sub-xxxxxx`).

**Response Example:**
```json
{
  "id": "sub-e91760a84f01",
  "status": "destroyed"
}
```

---

### 5. `packets_build`
Dispatches a remote compilation task to the scheduler or a designated Subspace.

**Parameters:**
- `toolchain` (*string, required*): Language compiler toolchain (`android`, `rust`, `go`, etc.).
- `runner` (*string, optional*): `host`, `docker`, or `github` (default: `docker`).
- `subspace_id` (*string, optional*): Target persistent Subspace identifier.
- `command_args` (*array[string], optional*): Custom arguments passed to the build tool.
- `artifact_paths` (*array[string], optional*): Glob patterns for artifact harvesting.

**Response Example:**
```json
{
  "job_id": "j_aaff49a2",
  "cache_hit": false,
  "state": "submitted"
}
```

---

### 6. `packets_job_status`
Queries the execution status, exit code, and artifact references for a build job.

**Parameters:**
- `job_id` (*string, required*): The unique job identifier.

**Response Example:**
```json
{
  "job_id": "j_aaff49a2",
  "state": "JOB_STATE_SUCCEEDED",
  "artifact_ref": "default/artifacts/j_aaff49a2/output.tar.gz"
}
```

---

### 7. `packets_get_logs`
Fetches all collected stdout and stderr log lines for a job.

**Parameters:**
- `job_id` (*string, required*): The job identifier.

---

### 8. `packets_list_workers`
Lists all active, healthy, or draining worker nodes registered with the scheduler.

**Response Example:**
```json
[
  {
    "id": "vps-worker",
    "healthy": true,
    "active_jobs": 0,
    "max_jobs": 5,
    "capabilities": ["android", "rust", "go", "docker"]
  }
]
```
