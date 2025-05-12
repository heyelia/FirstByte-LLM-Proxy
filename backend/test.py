import asyncio
import json
import os
import time
from datetime import datetime

import dotenv
import numpy as np
import openai
import pytest

from plot_results import generate_cdf_chart

dotenv.load_dotenv()

direct_latencies = []
proxy_latencies = []

direct_client = openai.AsyncOpenAI(
    api_key=os.getenv("OPENAI_API_KEY"), base_url="https://api.openai.com/v1", timeout=60.0
)

proxy_client = openai.AsyncOpenAI(api_key="dummy-key", base_url="http://localhost:8080/v1")


def load_prompts():
    with open("./prompts.json", "r") as f:
        prompts = json.load(f)
    return prompts


async def make_request(client, prompt_text, is_proxy=False):
    start_time = time.time()

    try:
        response = await client.chat.completions.create(
            model="gpt-4o-mini",
            stream=True,
            messages=[
                {"role": "system", "content": "You are a helpful assistant."},
                {"role": "user", "content": prompt_text},
            ],
        )

        end_time = time.time()
        latency = (end_time - start_time) * 1000

        if is_proxy:
            proxy_latencies.append(latency)
            log_entry = {
                "timestamp": datetime.now().isoformat(),
                "message": "request race finished",
                "time_taken": latency,
                "prompt": prompt_text[:30] + "..." if len(prompt_text) > 30 else prompt_text,
            }
            with open("proxy_latencies.json", "a") as f:
                f.write(json.dumps(log_entry) + "\n")
        else:
            direct_latencies.append(latency)
            log_entry = {
                "timestamp": datetime.now().isoformat(),
                "message": "makeRequest completed",
                "fetchTime": latency,
                "prompt": prompt_text[:30] + "..." if len(prompt_text) > 30 else prompt_text,
            }
            with open("direct_latencies.json", "a") as f:
                f.write(json.dumps(log_entry) + "\n")

        with open("out.log", "a") as f:
            f.write(json.dumps(log_entry) + "\n")

        return response

    except Exception as e:
        end_time = time.time()
        latency = (end_time - start_time) * 1000
        print(f"Error in {'proxy' if is_proxy else 'direct'} request: {e}")

        log_entry = {
            "timestamp": datetime.now().isoformat(),
            "message": f"{'request race' if is_proxy else 'makeRequest'} error",
            "error": str(e),
            "time_taken": latency,
            "prompt": prompt_text[:30] + "..." if len(prompt_text) > 30 else prompt_text,
        }

        file_name = "proxy_latencies.json" if is_proxy else "direct_latencies.json"
        with open(file_name, "a") as f:
            f.write(json.dumps(log_entry) + "\n")

        with open("out.log", "a") as f:
            f.write(json.dumps(log_entry) + "\n")

        raise


@pytest.mark.asyncio
async def test_direct_vs_proxy():
    prompts = load_prompts()
    print(f"Loaded {len(prompts)} prompts from prompts.json")

    with open("out.log", "w") as f:
        pass
    with open("proxy_latencies.json", "w") as f:
        pass
    with open("direct_latencies.json", "w") as f:
        pass

    print("\n--- Running Proxy Requests ---")
    for i, prompt_data in enumerate(prompts):
        prompt_text = prompt_data["text"]
        print(f"Running proxy request {i + 1}/{len(prompts)}: {prompt_text[:30]}...")

        try:
            await make_request(proxy_client, prompt_text, is_proxy=True)
        except Exception as e:
            print(f"Proxy request failed: {e}")

    print("\n--- Running Direct Requests ---")
    for i, prompt_data in enumerate(prompts):
        prompt_text = prompt_data["text"]
        print(f"Running direct request {i + 1}/{len(prompts)}: {prompt_text[:30]}...")

        try:
            await make_request(direct_client, prompt_text, is_proxy=False)
        except Exception as e:
            print(f"Direct request failed: {e}")

    if direct_latencies and proxy_latencies:
        generate_cdf_chart()

        print("\n--- Summary Statistics ---")
        print(
            f"Direct requests - Count: {len(direct_latencies)}, Mean: {np.mean(direct_latencies):.2f}ms, Median: {np.median(direct_latencies):.2f}ms"
        )
        print(
            f"Proxy requests - Count: {len(proxy_latencies)}, Mean: {np.mean(proxy_latencies):.2f}ms, Median: {np.median(proxy_latencies):.2f}ms"
        )
    else:
        print("Not enough data to generate chart. Check the error logs.")


if __name__ == "__main__":
    asyncio.run(test_direct_vs_proxy())
