import yaml

chapters = [
    "Introduction to Probability and Measure Theory",
    "Random Variables and Expectations",
    "Convergence of Random Variables",
    "Conditional Expectation and Martingales",
    "Markov Chains and Random Walks",
    "Continuous-Time Markov Processes",
    "Brownian Motion and Stochastic Calculus",
    "Information Theory and Entropy",
    "Bayesian Inference Fundamentals",
    "Advanced Markov Chain Monte Carlo (MCMC)",
    "Variational Inference and Optimization",
    "Time Series Analysis and Forecasting",
    "Spatial Statistics and Random Fields",
    "Deep Generative Models in Statistics"
]

nodes = []

# Node 1: Outline
nodes.append({
    "id": "book-outline",
    "agent_id": "architect-agent",
    "system_prompt": "You are the Chief Editor of an Advanced Statistics textbook. Write the comprehensive syllabus for the 14 chapters. Provide strict guidelines on mathematical rigor and formatting.",
    "user_prompt": "Generate the Advanced Statistics textbook syllabus and style guide.",
    "dependencies": []
})

# 14 Writers and 14 Reviewers = 28 Nodes
for i, title in enumerate(chapters):
    writer_id = f"writer-chapter-{i+1}"
    reviewer_id = f"reviewer-chapter-{i+1}"
    
    nodes.append({
        "id": writer_id,
        "agent_id": "writer-agent",
        "system_prompt": "You are an expert PhD-level Statistics author. Write a comprehensive, rigorous chapter in Markdown format using LaTeX math equations.",
        "user_prompt": f"Write Chapter {i+1}: {title}. Ensure rigorous proofs, examples, and deep intuition.",
        "dependencies": ["book-outline"]
    })
    
    nodes.append({
        "id": reviewer_id,
        "agent_id": "writer-agent", # Using writer agent as peer reviewer
        "system_prompt": "You are a PhD-level Peer Reviewer. You must critically audit the provided statistics chapter for mathematical errors, logical leaps, and formatting. You must provide a final polished version of the Markdown chapter.",
        "user_prompt": f"Review and polish Chapter {i+1}: {title}. Fix any mathematical mistakes and return the final complete chapter text.",
        "dependencies": [writer_id]
    })

# Node 30: LaTeX Compiler
latex_deps = [f"reviewer-chapter-{i+1}" for i in range(14)]
nodes.append({
    "id": "latex-compiler",
    "agent_id": "latex-agent",
    "system_prompt": "You are the LaTeX Engine. Combine all 14 reviewed chapters into a single stunning PDF book.",
    "user_prompt": "Combine all inputs into book.tex. Include TikZ diagrams for normal and poisson distributions in the intro chapter. Compile to PDF.",
    "dependencies": latex_deps
})

dag = {
    "workflow": {
        "id": "advanced-statistics-book",
        "name": "Advanced Statistics Textbook Generation Pipeline",
        "description": "Massive 30-node DAG compiling a 14-chapter PhD textbook in parallel with peer review and LaTeX typesetting.",
        "nodes": nodes
    }
}

with open("workflows/textbook.yaml", "w") as f:
    yaml.dump(dag, f, sort_keys=False, default_flow_style=False)
    
print("DAG successfully generated at workflows/textbook.yaml")
