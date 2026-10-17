---
name: hermes-repl
description: The read-eval-print loop for writing code that is proven to work: criteria, tests first, run, read the error, fix one thing, run again until it passes.
---

# Hermes: write, run, read, fix

You do not finish when the code looks right. You finish when it has **run and passed**, after your last change. `mark_task_complete` is refused until `execute_terminal_command` has returned exit code 0 since your last edit.

## The loop

1. **Criteria.** Restate the requirement as a short list of checks that can pass or fail ("`add(2, 3)` returns 5", "the CLI exits 1 on a missing file"). Ambiguity you cannot resolve from the task: choose the simplest reading and say so in your summary.
2. **Tests first.** Write the checks as a runnable test file with `unittest` (always available): `python -m unittest -v`. Run it once before the implementation so you see it fail for the right reason.
3. **Implement the smallest thing** that could pass. Write it, then run.
4. **Read the output from the bottom up.** For a traceback, the last line is the error and the frame above it is where it happened. Say in one sentence what is wrong and why.
5. **Change one thing, run again.** One hypothesis per run. If you change three things and it passes, you do not know which one mattered.
6. **Repeat until exit code 0**, then run the whole suite once more (not just the test you were fixing) to catch what you broke.
7. **Read the final files back** with `read_file`, then summarise.

## Rules of the terminal

- Each `execute_terminal_command` is independent: no `cd`, no exported variables and no installed packages carry over. Use a single command each time: `python -m unittest -v`, `python main.py`, `python -m py_compile file.py`.
- In container mode you have Python 3.12, pip and network, and no node, npm or git. For a package, put it in the same command: `uv run --with requests python main.py`. With native execution enabled the command runs on the user's machine instead.
- Commands are limited by a time limit and output is cut at 10,000 characters per stream. Make a test noisy only when you need to read it.
- Do not "fix" a failing test by weakening or deleting it unless the test itself is wrong, and say so when that is the case.

## When it does not converge

- The same error three times means your fixes do not address its cause. Stop patching that line. Re-read the file and the test, print the actual values the code receives (a temporary `print`, then remove it), or reproduce the problem in a ten-line script.
- If a run cannot happen at all (the tool reports the terminal could not run), say plainly in the summary that **nothing was executed** and that the code is unverified.
- If the requirement cannot be met as stated, say which part and why rather than passing a test that checks something else.

## The summary

State what was built, which command you ran last and that it passed (with the count of tests), what you changed along the way if it matters, and any limit you know of. Do not claim a check you did not run.
