---
name: verification-loop
description: How a worker proves its files work before it finishes, using only the tools it really has.
---

# Verification loop

Finish only when every file you were asked to produce has been checked. A file you wrote but did not read back is unverified.

1. **Read back.** Call `read_file` on each file you created or changed. Listing a directory, or running `ls` or `cat`, does not count. Fix what you find, then read the file again.
2. **Check the pieces fit.** Every name one file takes from another must exist there with the same spelling: script and stylesheet paths in HTML, imported and exported names, data keys, asset file names.
3. **Web output.** After your last edit, call `check_web_page`, fix every problem it lists, and run it again until it reports none. If you also have `run_page`, call it after that and fix every exception, failed request and blank-screen verdict it reports.
4. **Commands.** If you have `execute_terminal_command` and the project defines a build, test or lint command (for example `npm run build`, `python -m pytest`, `node --check file.js`), run it. A failing command means the task is not done. Do not invent commands the project does not define.
5. **Report.** Call `mark_task_complete` with what you produced, what you checked and how, and anything you could not verify. State a failure plainly instead of claiming success.
