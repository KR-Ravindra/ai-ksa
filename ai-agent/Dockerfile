FROM python:3.11-slim
ADD ./requirements.txt /app/requirements.txt
ADD . /app
WORKDIR /app
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "ai_scaling_agent.py"]