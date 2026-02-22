# Coding style

Comments end with a full stop.

Constructed URLs need to use path escaping: fmt.Sprintf("/flows/%s/delete", url.PathEscape(flow.ID)).

After creating new files or updating previous ones, check that the file isn't corrupted.

Test names are positive assertions of behaviour, e.g. "functions can return multiple values".

Use subtests, and table-driven tests where appropriate.

Avoid the use of interface{} - in modern Go, use any.

Avoid for i := 0; i < 10; i++ - modern Go supports the for range 10 construct, and b.Loop() in benchmarks.

Use xc -s to list all tasks, e.g. compiling, running, testing.
