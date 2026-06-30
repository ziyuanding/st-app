package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSqJobChainGroupsUsesDependenciesBetweenAdjacentJobs(t *testing.T) {
	jobs := []Job{
		{ID: "964944", Dependency: "afterok:964943"},
		{ID: "964943", Dependency: "afterok:964942"},
		{ID: "964942", Dependency: "afterok:964941"},
		{ID: "964940"},
		{ID: "964939"},
	}

	got := sqJobChainGroups(jobs)
	want := []int{0, 0, 0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sqJobChainGroups() = %v, want %v", got, want)
	}
}

func TestSqJobChainGroupsUsesSubmitLineDependenciesForCompletedJobs(t *testing.T) {
	jobs := []Job{
		{ID: "965035", Name: "run_refseq_species_grad_classify_eval", Dependency: "afterok:965034(unfulfilled)"},
		{ID: "965034", Name: "run_refseq_species_grad_classify_embeddings", Dependency: "afterok:965033(unfulfilled)"},
		{ID: "965033", Name: "run_refseq_species_grad_classify_process", Dependency: "afterok:965032"},
		{ID: "965032", Name: "run_refseq_species_grad_classify_download", Dependency: "afterok:965031"},
		{ID: "965031", Name: "run_refseq_species_grad_classify_prepare"},
		{ID: "964942", Name: "run_refseq_species_grad_classify_process"},
	}

	got := sqJobChainGroups(jobs)
	want := []int{0, 0, 0, 0, 0, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sqJobChainGroups() = %v, want %v", got, want)
	}
}

func TestParseDependencyFromSubmitLine(t *testing.T) {
	submitLine := "sbatch --parsable --job-name=run_refseq_species_grad_classify_download --dependency=afterok:965031 --kill-on-invalid-dep=yes /path/job.slurm"

	got := parseDependencyFromSubmitLine(submitLine)
	if got != "afterok:965031" {
		t.Fatalf("parseDependencyFromSubmitLine() = %q, want %q", got, "afterok:965031")
	}
}

func TestRenderSqViewDoesNotShowDependencyColumn(t *testing.T) {
	data := sqViewData{
		displayUser: "zding",
		jobs: []Job{{
			ID:           "964942",
			Name:         "process",
			State:        "PENDING",
			Part:         "cpu",
			Dependency:   "afterok:964941",
			NodeOrReason: "(Dependency)",
			Elapsed:      "00:00:00",
			Start:        "Unknown",
			End:          "Unknown",
		}},
	}

	output := renderSqView(data, 180, 0, 0).text
	if strings.Contains(output, "DEPENDS") || strings.Contains(output, "afterok:964941") {
		t.Fatalf("dependency details should not be shown:\n%s", output)
	}
}

func TestRenderSqViewExpandsJobNameWhenThereIsRoom(t *testing.T) {
	longName := "run_refseq_species_grad_classify_embeddings_with_long_suffix"
	data := sqViewData{
		displayUser: "zding",
		jobs: []Job{{
			ID:           "965034",
			Name:         longName,
			State:        "PENDING",
			Part:         "gpu",
			NodeOrReason: "(Dependency)",
			Elapsed:      "00:00:00",
			Start:        "Unknown",
			End:          "Unknown",
		}},
	}

	output := renderSqView(data, 220, 0, 0).text
	if !strings.Contains(output, longName) {
		t.Fatalf("expected full job name in wide output:\n%s", output)
	}
}
