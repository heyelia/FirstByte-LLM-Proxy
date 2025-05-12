import json

import matplotlib.pyplot as plt
import numpy as np


def compute_cdf(data):
    sorted_data = np.sort(data)
    y = np.arange(1, len(sorted_data) + 1) / len(sorted_data)
    return sorted_data, y


def calc_percent_diff(val1, val2):
    return ((val1 - val2) / val1) * 100


def load_latencies_from_files():
    direct_latencies = []
    proxy_latencies = []

    try:
        with open("direct_latencies.json", "r") as f:
            for line in f:
                try:
                    data = json.loads(line)
                    if "fetchTime" in data:
                        direct_latencies.append(data["fetchTime"])
                except json.JSONDecodeError:
                    continue
    except FileNotFoundError:
        print("direct_latencies.json not found")

    try:
        with open("proxy_latencies.json", "r") as f:
            for line in f:
                try:
                    data = json.loads(line)
                    if "time_taken" in data:
                        proxy_latencies.append(data["time_taken"])
                except json.JSONDecodeError:
                    continue
    except FileNotFoundError:
        print("proxy_latencies.json not found")

    return direct_latencies, proxy_latencies


def generate_cdf_chart():
    direct_latencies, proxy_latencies = load_latencies_from_files()

    if not direct_latencies or not proxy_latencies:
        print("Not enough data to generate chart. Check if the JSON files exist and contain valid data.")
        return

    x1, y1 = compute_cdf(direct_latencies)
    x2, y2 = compute_cdf(proxy_latencies)

    median_diff = calc_percent_diff(np.median(direct_latencies), np.median(proxy_latencies))
    p95_diff = calc_percent_diff(np.percentile(direct_latencies, 95), np.percentile(proxy_latencies, 95))
    p99_diff = calc_percent_diff(np.percentile(direct_latencies, 99), np.percentile(proxy_latencies, 99))
    p9999_diff = calc_percent_diff(np.percentile(direct_latencies, 99.99), np.percentile(proxy_latencies, 99.99))
    max_diff = calc_percent_diff(np.max(direct_latencies), np.max(proxy_latencies))

    plt.figure(figsize=(10, 6))
    plt.plot(x1, y1, label="Direct request latency", color="blue")
    plt.plot(x2, y2, label="Proxy request latency", color="red")

    plt.xlabel("Latency (ms)")
    plt.ylabel("Cumulative Density")
    plt.title("CDF of Request Latencies (OpenAI API)")
    plt.grid(True, alpha=0.3)
    plt.legend()
    plt.xlim(0, 5000)

    stats_text = (
        f"Direct request latency:\n"
        f"  median: {np.median(direct_latencies):.1f}ms\n"
        f"  95th: {np.percentile(direct_latencies, 95):.1f}ms\n"
        f"  99th: {np.percentile(direct_latencies, 99):.1f}ms\n"
        f"  99.99th: {np.percentile(direct_latencies, 99.99):.1f}ms\n"
        f"  max: {np.max(direct_latencies):.1f}ms\n\n"
        f"Proxy request latency:\n"
        f"  median: {np.median(proxy_latencies):.1f}ms\n"
        f"  95th: {np.percentile(proxy_latencies, 95):.1f}ms\n"
        f"  99th: {np.percentile(proxy_latencies, 99):.1f}ms\n"
        f"  99.99th: {np.percentile(proxy_latencies, 99.99):.1f}ms\n"
        f"  max: {np.max(proxy_latencies):.1f}ms\n\n"
        f"Percentage speed up:\n"
        f"  median: {median_diff:+.1f}%\n"
        f"  95th: {p95_diff:+.1f}%\n"
        f"  99th: {p99_diff:+.1f}%\n"
        f"  99.99th: {p9999_diff:+.1f}%\n"
        f"  max: {max_diff:+.1f}%"
    )
    plt.text(
        0.02,
        0.02,
        stats_text,
        transform=plt.gca().transAxes,
        bbox=dict(facecolor="white", alpha=0.8),
        fontsize=8,
        family="monospace",
    )

    plt.tight_layout()
    plt.savefig("openai_latency_comparison.png")
    plt.close()


if __name__ == "__main__":
    generate_cdf_chart()
