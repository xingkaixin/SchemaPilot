# Resume creates attempts inside the same run

A Migration Run represents the operator's logical intent, while each initial or resumed invocation is a Run Attempt. Resuming appends Node Executions and Script Executions instead of overwriting them or creating an unrelated run, preserving an auditable history without making operators correlate multiple runs manually.
