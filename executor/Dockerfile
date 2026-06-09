FROM python:3.12-slim

WORKDIR /app

RUN pip install --no-cache-dir requests pyyaml

COPY executor.py .

USER 65532:65532

ENTRYPOINT ["python3", "executor.py"]
