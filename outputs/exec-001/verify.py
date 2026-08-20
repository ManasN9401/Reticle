import sys

def verify_files():
    files = ["event_spec.txt", "poem.md", "references.md"]
    for f in files:
        try:
            with open(f, "r", encoding="utf-8") as file:
                content = file.read()
                print(f"[OK] Successfully verified {f} (Length: {len(content)} chars)")
        except Exception as e:
            print(f"[ERROR] Failed to read {f}: {e}")
            sys.exit(1)
    print("All files verified successfully using Python 3!")

if __name__ == "__main__":
    verify_files()
