import json

def calculate_throughput(filename):
    with open(filename, 'r') as f:
        data = json.load(f)

    total_commands = 0
    total_duration = 0
    count = 0

    for record in data:
        if record.get('@type') == 'type.googleapis.com/types.ThroughputMeasurement':
            total_commands += int(record.get('Commands', 0))
            duration_str = record.get('Duration', '0s')
            duration_s = float(duration_str.replace('s', ''))
            total_duration += duration_s
            count += 1

    if total_duration == 0:
        return 0

    return total_commands / total_duration

in_memory_throughput = calculate_throughput('in-memory-results/local/measurements.json')
mdbx_throughput = calculate_throughput('mdbx-results/local/measurements.json')

print(f"In-memory throughput: {in_memory_throughput:.2f} commands/sec")
print(f"MDBX throughput: {mdbx_throughput:.2f} commands/sec")
