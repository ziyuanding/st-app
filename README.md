# Slurm TUI (st)

A Terminal User Interface (TUI) for Slurm workload manager, written in **Go**. This tool provides a more intuitive way to monitor partitions and job details directly from your terminal.

## Features

  * **Partition Overview**: Quickly list all partitions and their current states.
  * **Detailed Inspection**: View specific details for a chosen partition.

## Installation

### Build from Source

```bash
git clone https://github.com/ziyuanding/st-app.git
cd st-app
go build -o st
```

## Usage

| Command | Description |
| :--- | :--- |
| `./st lsp` | List all partitions and an overview of their status. |
| `./st -p <partition_name>` | Show detailed information for a specific partition. |

## Screenshots

### Partition List (`./st lsp`)

<img width="2966" height="1440" alt="Gemini_Generated_Image_j3j8ubj3j8ubj3j8" src="https://github.com/user-attachments/assets/cdb4e2a6-5b32-4c6f-aaef-7bff80ed3203" />


### Partition Details (`./st -p <partition_name>`)

<img width="2608" height="1632" alt="Gemini_Generated_Image_hsik6rhsik6rhsik" src="https://github.com/user-attachments/assets/a527de3b-232d-41d8-87d2-b93fdc9e6084" />

