# Code Review Guidelines

Define the rules and standards you want the agent to check during the `/commit` workflow. 

### Examples of things you might want to enforce:
- **Style:** Are function names communicative? Are we using proper Go error wrapping (`fmt.Errorf("...: %w", err)`)?
- **Architecture:** Are database calls isolated from discord handlers? Is dependency injection used properly?
- **Safety:** Are all `goroutine`s handling panics? Are SQL queries safe from injection?
- **Completeness:** Are new configurations added to `.env.example`?

---

### Add your rules below:
1. If there are more standard go practices that you think should be followed, suggest them with a good explanation.
2. Otherwise, basic lint/logic checks 
