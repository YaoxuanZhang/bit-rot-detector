# Contributing to Bit Rot Detector

Thank you for your interest in contributing to Bit Rot Detector!

## Getting Started

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/jason/bit-rot-detector.git
    cd bit-rot-detector
    ```

2.  **Install dependencies using `uv`**:
    This project uses [uv](https://github.com/astral-sh/uv) for fast dependency management.
    ```bash
    uv sync --extra dev
    ```

## Development

### Running the Tool

You can run the tool locally using `uv`:

```bash
uv run bit-rot-detector --help
```

### Running Tests

We use `pytest` for testing.

```bash
# Run all tests
uv run pytest

# Run specific test
uv run pytest tests/test_config.py
```

Please ensure all tests pass before submitting a pull request.

## Code Style

- We follow standard Python PEP 8 guidelines.
- Please include type hints in your code.
- Add tests for any new features or bug fixes.

## Pull Request Process

1.  Create a new branch for your feature or fix.
2.  Make your changes and add tests.
3.  Run the full test suite to ensure no regressions.
4.  Submit a Pull Request with a clear description of your changes.

## License

By contributing, you agree that your contributions will be licensed under the project's MIT License.
