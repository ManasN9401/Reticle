# Credential exposure follow-up

Date confirmed: 9 September 2026.

A live-model memory evaluation revealed that maintained examples referenced a retired Groq model. The ensuing credential scan found two literal Groq API keys and one literal OpenRouter API key in tracked Python files. The currently configured primary Groq key matched one committed literal. Secret values are intentionally omitted from this report.

The working tree now reads provider credentials from environment variables only. The maintained book workflow uses a model confirmed in the live Groq catalogue on the assessment date, example workers accept `llm_model` or `RETICLE_DEFAULT_MODEL`, and a regression test rejects common literal Groq, OpenRouter and Google API-key forms in source files.

This source edit does not remove secrets from existing Git objects, forks, clones, logs or provider records. Revoke both exposed Groq keys and the exposed OpenRouter key at their providers, issue replacements, and update the untracked `.env`. The active Groq replacement is required before relying on Groq-backed Reticle work. Review provider usage logs from the first commit containing each key through revocation.

After rotation, decide whether repository history must be rewritten. A history rewrite changes commit IDs and requires coordinated force updates for every clone and branch; do not treat it as a substitute for revocation. If the repository has ever been public or shared, assume the old values were copied.
