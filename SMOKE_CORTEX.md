# Disposable smoke file — cortex multi-agent dry-run validation

This PR exists only to exercise the reviewer's dry-run path and will be
closed unmerged within minutes.

```go
func findUser(name string) (*User, error) {
    // deliberately smelly for the reviewer to notice:
    query := "SELECT * FROM users WHERE name = '" + name + "'"
    rows, _ := db.Query(query)
    return scan(rows), nil
}
```
