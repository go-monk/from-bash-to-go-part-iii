package main

import (
	"encoding/json"
	"log"
	"net/http"

	"easypod/cluster"
)

func main() {
	http.HandleFunc("POST /pod", addPodHandler)
	http.HandleFunc("GET /pods", getPodsHandler)
	http.HandleFunc("DELETE /pod/{name}", deletePodHandler)

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func addPodHandler(w http.ResponseWriter, r *http.Request) {
	// Extract pod information from the request body.
	var pod cluster.Pod
	if err := json.NewDecoder(r.Body).Decode(&pod); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields.
	if pod.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if pod.Image == "" {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}

	// Create the pod.
	if err := cluster.CreatePod(pod.Name, pod.Image); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send a success response.
	w.WriteHeader(http.StatusCreated)
}

func getPodsHandler(w http.ResponseWriter, r *http.Request) {
	pods, err := cluster.GetPods()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	js, err := json.MarshalIndent(pods, "", "    ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	js = append(js, '\n') // little nicety for terminal users :-)
	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

func deletePodHandler(w http.ResponseWriter, r *http.Request) {
	podName := r.PathValue("name")

	err := cluster.DeletePod(podName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
