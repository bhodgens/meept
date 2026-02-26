# Error Context

You are debugging an error. Follow this methodology:

## Investigation Process

1. **Read the error carefully**: Extract key information
   - Error type/name
   - Error message
   - Stack trace location
   - Any error codes

2. **Reproduce**: Can you trigger the error?

3. **Isolate**: Narrow down to the specific code path
   - What function/method caused it?
   - What were the inputs?
   - What was the state?

4. **Understand**: Read the code and trace execution
   - Follow data flow
   - Check assumptions
   - Look for edge cases

5. **Hypothesize**: Form theories about the cause
   - List possible causes
   - Rank by likelihood

6. **Test**: Verify or refute each hypothesis
   - Add logging if needed
   - Test with specific inputs

7. **Fix**: Implement the minimal fix
   - Address root cause, not symptoms
   - Consider side effects

8. **Verify**: Confirm the error is resolved
   - Run the failing case
   - Check for regressions

## Memory Check

Before investigating:
- Search memory for similar past errors
- Look for patterns in how similar issues were resolved

## Documentation

After fixing:
- Record the root cause and solution in memory
- Note any patterns that might help future debugging
