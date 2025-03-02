package workloads_test

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	korifiv1alpha1 "code.cloudfoundry.org/korifi/controllers/api/v1alpha1"
	"code.cloudfoundry.org/korifi/tools/k8s"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CFSpaceValidatingWebhook", func() {
	var (
		ctx               context.Context
		cfSpace, cfSpace2 *korifiv1alpha1.CFSpace
		orgNamespace      string
	)

	BeforeEach(func() {
		ctx = context.Background()

		orgNamespace = "test-org-" + uuid.NewString()
		Expect(k8sClient.Create(ctx, &korifiv1alpha1.CFOrg{
			ObjectMeta: metav1.ObjectMeta{
				Name:      orgNamespace,
				Namespace: rootNamespace,
			},
			Spec: korifiv1alpha1.CFOrgSpec{
				DisplayName: orgNamespace,
			},
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   orgNamespace,
				Labels: map[string]string{korifiv1alpha1.OrgNameLabel: orgNamespace},
			},
		})).To(Succeed())

		// Ensure that the client used by the validating webhook has the CFOrg in its cache.
		// In practice this will always be the case because...
		//   1. The controllers and webhooks share the same client
		//   2. The CFSpace is created in the namespace that the CFOrgReconciler creates
		//   3. To create the namespace, the CFOrgReconciler has to first fetch the CFOrg (ensuring it's in the cache)
		Eventually(func() error {
			return internalWebhookK8sClient.Get(ctx, types.NamespacedName{Name: orgNamespace, Namespace: rootNamespace}, new(korifiv1alpha1.CFOrg))
		}).Should(Succeed())
	})

	Describe("creating a space", func() {
		var err error

		BeforeEach(func() {
			cfSpace = makeCFSpace(orgNamespace, "my-space")
		})

		JustBeforeEach(func() {
			err = k8sClient.Create(ctx, cfSpace)
		})

		It("succeeds", func() {
			Expect(err).To(Succeed())
		})

		When("a corresponding CFOrg does not exist", func() {
			BeforeEach(func() {
				cfSpace.Namespace = "not-an-org"
				Expect(k8sClient.Create(ctx, &v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-an-org",
					},
				})).To(Succeed())
			})

			It("fails", func() {
				Expect(err).To(MatchError(ContainSubstring("Organization 'not-an-org' does not exist for Space 'my-space'")))
			})
		})

		When("the name already exists in the org namespace", func() {
			BeforeEach(func() {
				cfSpace2 = makeCFSpace(orgNamespace, "my-space")
				Expect(k8sClient.Create(ctx, cfSpace2)).To(Succeed())
			})

			It("fails", func() {
				Expect(err).To(MatchError(ContainSubstring("Name must be unique per organization")))
			})
		})

		When("another CFSpace exists with the same name(case insensitive) in the same namespace", func() {
			BeforeEach(func() {
				cfSpace2 = makeCFSpace(orgNamespace, "My-Space")
				Expect(k8sClient.Create(ctx, cfSpace2)).To(Succeed())
			})

			It("should fail", func() {
				Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("Space '%s' already exists.", cfSpace.Spec.DisplayName))))
			})
		})
	})

	Describe("updating a space", func() {
		BeforeEach(func() {
			cfSpace = makeCFSpace(orgNamespace, "my-space")
			Expect(k8sClient.Create(ctx, cfSpace)).To(Succeed())
		})

		When("the space name is changed to another which is unique in the root CF namespace", func() {
			It("succeeds", func() {
				Expect(k8s.Patch(ctx, k8sClient, cfSpace, func() {
					cfSpace.Spec.DisplayName = "another-space"
				})).To(Succeed())
			})
		})

		When("the new space name already exists in the org namespace", func() {
			BeforeEach(func() {
				cfSpace2 = makeCFSpace(orgNamespace, "another-space")
				Expect(k8sClient.Create(ctx, cfSpace2)).To(Succeed())
			})

			It("fails", func() {
				Expect(k8s.Patch(ctx, k8sClient, cfSpace, func() {
					cfSpace.Spec.DisplayName = "another-space"
				})).To(MatchError(ContainSubstring("Name must be unique per organization")))
			})
		})
	})

	Describe("deleting a space", func() {
		var err error
		BeforeEach(func() {
			cfSpace = makeCFSpace(orgNamespace, "my-space")
			err = k8sClient.Create(ctx, cfSpace)
			Expect(err).NotTo(HaveOccurred())
		})

		It("can delete the space", func() {
			Expect(k8sClient.Delete(ctx, cfSpace)).To(Succeed())
		})
	})
})

func makeCFSpace(namespace string, displayName string) *korifiv1alpha1.CFSpace {
	return &korifiv1alpha1.CFSpace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.NewString(),
			Namespace: namespace,
			Labels:    map[string]string{korifiv1alpha1.SpaceNameLabel: displayName},
		},
		Spec: korifiv1alpha1.CFSpaceSpec{
			DisplayName: displayName,
		},
	}
}
