# Setting up GHCR Pull Secret

The Karpenter pod is failing to pull the image from GitHub Container Registry (ghcr.io) due to missing authentication.

## Steps to Create Pull Secret

1. **Create a GitHub Personal Access Token (PAT)**:
   - Go to GitHub Settings > Developer settings > Personal access tokens > Tokens (classic)
   - Click "Generate new token"
   - Select scope: `read:packages`
   - Save the token

2. **Create the Docker Registry Secret**:
   ```bash
   kubectl create secret docker-registry ghcr-pull-secret \
     --docker-server=ghcr.io \
     --docker-username=<your-github-username> \
     --docker-password=<your-github-pat> \
     --docker-email=<your-email> \
     -n karpenter
   ```

3. **Update the Helm values to use the secret**:
   
   In your FluxCD repository, update the HelmRelease to include:
   ```yaml
   spec:
     values:
       imagePullSecrets:
         - name: ghcr-pull-secret
   ```

## Alternative: Using Sealed Secrets

If you prefer to use Sealed Secrets (recommended for GitOps):

1. **Create the docker-registry secret locally**:
   ```bash
   kubectl create secret docker-registry ghcr-pull-secret \
     --docker-server=ghcr.io \
     --docker-username=<your-github-username> \
     --docker-password=<your-github-pat> \
     --docker-email=<your-email> \
     --dry-run=client -o yaml > ghcr-pull-secret.yaml
   ```

2. **Seal the secret**:
   ```bash
   kubeseal --format=yaml < ghcr-pull-secret.yaml > ghcr-pull-secret-sealed.yaml
   ```

3. **Add to your FluxCD repository** and reference in HelmRelease as above.

## Current Issue

The pod is trying to pull: `ghcr.io/startappdev/karpenter:start-io-bd72908`

Once you've created the pull secret and updated the HelmRelease, the pod should be able to pull the image and start.