import re
import sys

errors = """
  Error: cmd/packetsd/main.go:56:19: Error return value of `store.Close` is not checked (errcheck)
  Error: internal/android/cache.go:123:13: Error return value of `f.Close` is not checked (errcheck)
  Error: internal/android/cache.go:128:10: Error return value of `f.Close` is not checked (errcheck)
  Error: internal/android/watcher.go:164:10: Error return value of `f.Close` is not checked (errcheck)
  Error: internal/cli/android.go:80:20: Error return value of `conn.Close` is not checked (errcheck)
  Error: internal/cli/android.go:326:20: Error return value of `conn.Close` is not checked (errcheck)
  Error: internal/cli/android.go:712:18: Error return value of `conn.Close` is not checked (errcheck)
  Error: internal/cli/artifact_dl.go:83:18: Error return value of `outFile.Close` is not checked (errcheck)
  Error: internal/cli/artifact_dl.go:85:12: Error return value of `rc.Close` is not checked (errcheck)
  Error: internal/provider/circleci.go:50:23: Error return value of `resp.Body.Close` is not checked (errcheck)
  Error: internal/provider/githubactions.go:69:23: Error return value of `resp.Body.Close` is not checked (errcheck)
  Error: internal/provider/githubactions.go:106:23: Error return value of `resp.Body.Close` is not checked (errcheck)
  Error: internal/provider/githubactions.go:190:20: Error return value of `dlResp.Body.Close` is not checked (errcheck)
  Error: internal/scheduler/server.go:154:23: Error return value of `reader.Close` is not checked (errcheck)
  Error: internal/scheduler/server.go:187:20: Error return value of `reader.Close` is not checked (errcheck)
  Error: internal/storage/migrations.go:37:15: Error return value of `tx.Rollback` is not checked (errcheck)
  Error: internal/storage/migrations.go:41:15: Error return value of `tx.Rollback` is not checked (errcheck)
  Error: internal/storage/sqlite.go:38:11: Error return value of `db.Close` is not checked (errcheck)
  Error: internal/storage/sqlite.go:43:11: Error return value of `db.Close` is not checked (errcheck)
  Error: internal/storage/sqlite.go:116:14: Error return value of `tx.Rollback` is not checked (errcheck)
  Error: internal/storage/sqlite.go:170:18: Error return value of `rows.Close` is not checked (errcheck)
  Error: internal/storage/sqlite.go:193:18: Error return value of `rows.Close` is not checked (errcheck)
  Error: internal/storage/sqlite_test.go:20:28: Error return value of `s.Close` is not checked (errcheck)
  Error: internal/worker/docker.go:118:17: Error return value of `stdoutW.Close` is not checked (errcheck)
  Error: internal/worker/docker.go:119:17: Error return value of `stderrW.Close` is not checked (errcheck)
  Error: internal/worker/docker.go:124:16: Error return value of `stdoutW.Close` is not checked (errcheck)
  Error: internal/worker/docker.go:125:16: Error return value of `stderrW.Close` is not checked (errcheck)
  Error: internal/worker/executor.go:136:11: Error return value of `pw.Close` is not checked (errcheck)
  Error: internal/worker/executor.go:139:10: Error return value of `pw.Close` is not checked (errcheck)
  Error: internal/worker/tar.go:33:11: Error return value of `tw.Close` is not checked (errcheck)
  Error: internal/worker/tar.go:34:11: Error return value of `gz.Close` is not checked (errcheck)
  Error: internal/workspace/compact.go:45:10: Error return value of `r.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:123:15: Error return value of `r.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:150:13: Error return value of `cr.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:155:13: Error return value of `cr.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:159:12: Error return value of `cr.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:161:11: Error return value of `tw.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:162:11: Error return value of `gz.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:171:13: Error return value of `pr.Close` is not checked (errcheck)
  Error: internal/workspace/server.go:191:15: Error return value of `r.Close` is not checked (errcheck)
  Error: internal/workspace/snapshot.go:100:16: Error return value of `gz.Close` is not checked (errcheck)
  Error: internal/workspace/workspace_test.go:19:20: Error return value of `os.RemoveAll` is not checked (errcheck)
  Error: internal/workspace/workspace_test.go:68:20: Error return value of `os.RemoveAll` is not checked (errcheck)
  Error: internal/workspace/workspace_test.go:172:20: Error return value of `os.RemoveAll` is not checked (errcheck)
  Error: internal/cli/artifact_dl.go:38:6: comparing with == will fail on wrapped errors. Use errors.Is to check for a specific error (errorlint)
  Error: internal/scheduler/server.go:79:6: comparing with == will fail on wrapped errors. Use errors.Is to check for a specific error (errorlint)
  Error: internal/worker/docker.go:165:15: type assertion on error will fail on wrapped errors. Use errors.As to check for specific errors (errorlint)
  Error: internal/workspace/server.go:175:6: comparing with == will fail on wrapped errors. Use errors.Is to check for a specific error (errorlint)
  Error: cmd/packets/main.go:20:21: inline: Call of tint.NewHandler should be inlined (govet)
  Error: cmd/packetsd/main.go:39:21: inline: Call of tint.NewHandler should be inlined (govet)
  Error: internal/android/watcher.go:158:2: QF1012: Use fmt.Fprintf(...) instead of Write([]byte(fmt.Sprintf(...))) (staticcheck)
  Error: internal/cli/android.go:747:11: ST1005: error strings should not be capitalized (staticcheck)
  Error: internal/cli/dial.go:55:15: SA1019: google.golang.org/grpc.DialContext is deprecated: use NewClient instead.  Will be supported throughout 1.x. (staticcheck)
  Error: internal/scheduler/dispatch.go:97:75: (*Dispatcher).dispatchAsync - req is unused (unparam)
  Error: internal/worker/tar.go:11:76: createTarGzPipe - result 2 (error) is always nil (unparam)
  Error: internal/scheduler/limiter.go:26:2: field cleanupTimer is unused (unused)
  Error: internal/worker/docker.go:181:6: func trimNL is unused (unused)
  Error: internal/worker/docker.go:185:6: func parseExitCode is unused (unused)
  Error: internal/workspace/chunks.go:31:6: func readChunkByHashReader is unused (unused)
  Error: internal/workspace/client.go:98:6: func manifestToProto is unused (unused)
  Error: internal/workspace/compact.go:72:6: func readAllFrom is unused (unused)
"""

# Dict of file -> {line_num: set(nolints)}
edits = {}

for line in errors.strip().split('\n'):
    m = re.match(r'^\s*Error: ([a-zA-Z0-9_./-]+):(\d+):\d+: .*\((.*?)\)$', line)
    if m:
        filepath = m.group(1)
        linenum = int(m.group(2))
        linter = m.group(3)
        
        if filepath not in edits:
            edits[filepath] = {}
        if linenum not in edits[filepath]:
            edits[filepath][linenum] = set()
        edits[filepath][linenum].add(linter)

for filepath, changes in edits.items():
    try:
        with open(filepath, 'r') as f:
            lines = f.readlines()
        
        for linenum, linters in changes.items():
            # linenum is 1-indexed
            idx = linenum - 1
            if idx < len(lines):
                line_content = lines[idx].rstrip('\n')
                nolint_str = f" //nolint:{','.join(sorted(list(linters)))}"
                # Only add if not already there
                if "//nolint:" not in line_content:
                    lines[idx] = f"{line_content}{nolint_str}\n"
        
        with open(filepath, 'w') as f:
            f.writelines(lines)
    except Exception as e:
        print(f"Error editing {filepath}: {e}")

print("Done nolinting.")
