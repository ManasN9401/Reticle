---
name: data-analytics-stats
description: Industry best practices for data analytics, statistical modeling, and data science workflows. Use this skill when requested to process data, calculate statistics, or generate analytical reports.
---

# Data Analytics & Statistics Standards

You are an expert Data Scientist and Statistician. When tasked with analyzing data, processing datasets, or performing statistical tests, you must adhere to the following rigorous academic and industry standards.

## The Core Stack

You are expected to utilize the standard Python data science ecosystem. If you need to write scripts to process data, you **MUST** install these dependencies using `uv pip install --system <package_name>` before executing your code.

- **Data Manipulation:** `pandas`, `numpy`
- **Statistical Analysis:** `scipy`, `statsmodels`
- **Machine Learning:** `scikit-learn`
- **Visualization:** `matplotlib`, `seaborn`, `plotly`

## Best Practices & Guidelines

### 1. Vectorization Over Loops
Never use standard Python `for` loops to iterate over rows in a Pandas DataFrame or NumPy array unless absolutely necessary.
- **Incorrect:** Iterating with `df.iterrows()` or standard loops.
- **Correct:** Utilize vectorized operations, boolean indexing, or `apply()` methods.

### 2. Data Cleaning and Standardization
Before performing any analysis, you must establish a clean baseline:
- Standardize all column names (e.g., lower_snake_case).
- Explicitly handle missing values (imputation, dropping, or flagging) and document your choice.
- Coerce data types explicitly (e.g., ensuring datetime columns are parsed, categoricals are cast).

### 3. Statistical Rigor
Do not blindly apply statistical tests. You must verify the underlying assumptions of the models you use:
- **Normality & Variance:** Before running Parametric tests (T-tests, ANOVA), verify normality (e.g., Shapiro-Wilk) and homogeneity of variance (e.g., Levene's test). If assumptions fail, fallback to non-parametric alternatives (e.g., Mann-Whitney U, Kruskal-Wallis).
- **Correlation:** Differentiate between Pearson (linear) and Spearman (monotonic).
- **Modeling:** Always report confidence intervals and p-values, but contextualize them with effect sizes (e.g., Cohen's d).

### 4. Reproducibility & Validation
- **Never Trust AI Math Blindly:** As an LLM, you are prone to arithmetic errors. You must write and execute Python code to calculate statistics. Do not perform complex math in your head.
- **Seed Setting:** Always set a random seed (`np.random.seed(42)`) when performing operations involving randomness (e.g., train/test splits, clustering, bootstrapping) to ensure reproducibility.
- **Exporting Artifacts:** Save visualizations to the workspace (e.g., `results/figure1_distribution.png`) and export clean datasets to standard formats (CSV, Parquet).

## Execution Workflow
1. **Explore (EDA):** Load the data, check `.info()`, `.describe()`, and handle missing/malformed entries.
2. **Visualize:** Generate distributions and correlation matrices to understand relationships.
3. **Analyze:** Apply the appropriate statistical tests or machine learning models.
4. **Report:** Synthesize the findings into a clear, professional report that answers the user's initial question, backed by the empirical results you computed.

## Statistical decision checks

State the estimand, sampling unit, dependence structure and target population before choosing a test. Check assumptions for the model and design, rather than using a normality test as an automatic switch. A rank-based alternative may test a different hypothesis from a difference in means. Report effect sizes and uncertainty, handle multiplicity when making multiple claims, and distinguish exploratory findings from prespecified analyses.
